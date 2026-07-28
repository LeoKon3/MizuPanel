package servicecenter

import (
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	serverdb "github.com/mizupanel/mizupanel/internal/server/db"
)

func newServiceCenterTestStore(t *testing.T) (*Store, *sql.DB) {
	t.Helper()
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}
	return NewStore(database, serverdb.DialectSQLite), database
}

func TestStoreCRUDNormalizationConflictRollbackAndCascade(t *testing.T) {
	store, database := newServiceCenterTestStore(t)
	ctx := t.Context()

	empty, err := store.List(ctx)
	if err != nil {
		t.Fatalf("list empty services: %v", err)
	}
	if empty == nil || len(empty) != 0 {
		t.Fatalf("empty services = %#v, want non-nil empty slice", empty)
	}

	created, err := store.Create(ctx, ServiceInput{
		Name:        "  MizuPanel  ",
		Description: "  internal operations  ",
		Resources: []Resource{
			{ResourceType: ResourceNode, ResourceKey: "node-missing", DisplayName: ""},
			{ResourceType: ResourceUptimeMonitor, ResourceKey: "0007", DisplayName: "Health"},
			{ResourceType: ResourceComposeProject, ScopeID: "node-missing", ResourceKind: "MANAGED", ResourceKey: "550E8400-E29B-41D4-A716-446655440000", DisplayName: "Panel Stack"},
		},
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	if created.ID == "" || created.Name != "MizuPanel" || created.Description != "internal operations" || len(created.Resources) != 3 {
		t.Fatalf("created service = %#v", created)
	}
	if _, err := time.Parse(time.RFC3339Nano, created.CreatedAt); err != nil {
		t.Fatalf("created_at is not RFC3339 UTC: %q: %v", created.CreatedAt, err)
	}
	if created.Resources[0].DisplayName == "" {
		t.Fatal("empty display name was not replaced by the resource key")
	}
	if created.Resources[1].ResourceKey != "7" {
		t.Fatalf("numeric resource key = %q, want canonical 7", created.Resources[1].ResourceKey)
	}
	if created.Resources[2].ResourceKind != "managed" || created.Resources[2].ResourceKey != "550e8400-e29b-41d4-a716-446655440000" {
		t.Fatalf("managed compose identity was not normalized: %#v", created.Resources[2])
	}

	loaded, err := store.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get service: %v", err)
	}
	if loaded.ID != created.ID || len(loaded.Resources) != 3 || loaded.Resources[0].ID == "" {
		t.Fatalf("loaded service = %#v", loaded)
	}

	other, err := store.Create(ctx, ServiceInput{Name: "Other"})
	if err != nil {
		t.Fatalf("create second service: %v", err)
	}
	_, err = store.Create(ctx, ServiceInput{Name: " mizupanel "})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("case-insensitive duplicate name error = %v, want ErrConflict", err)
	}

	_, err = store.Update(ctx, created.ID, ServiceInput{
		Name:      "OTHER",
		Resources: []Resource{{ResourceType: ResourceSystemdService, ScopeID: "node-new", ResourceKey: "panel.service"}},
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("conflicting update error = %v, want ErrConflict", err)
	}
	afterRollback, err := store.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get service after rollback: %v", err)
	}
	if afterRollback.Name != "MizuPanel" || len(afterRollback.Resources) != 3 {
		t.Fatalf("failed update changed persisted service: %#v", afterRollback)
	}

	updated, err := store.Update(ctx, created.ID, ServiceInput{
		Name:        "MizuPanel Core",
		Description: "updated",
		Resources:   []Resource{{ResourceType: ResourceSystemdService, ScopeID: "node-missing", ResourceKey: "mizupanel.service", DisplayName: "MizuPanel"}},
	})
	if err != nil {
		t.Fatalf("update service: %v", err)
	}
	if updated.CreatedAt != created.CreatedAt || updated.UpdatedAt == created.UpdatedAt || len(updated.Resources) != 1 {
		t.Fatalf("updated service = %#v", updated)
	}

	if _, err := database.Exec(`INSERT INTO nodes (id, name, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`, "original-node", "Original", "online", created.CreatedAt, created.CreatedAt); err != nil {
		t.Fatalf("insert original node: %v", err)
	}
	if err := store.Delete(ctx, created.ID); err != nil {
		t.Fatalf("delete service: %v", err)
	}
	if _, err := store.Get(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get deleted service error = %v, want ErrNotFound", err)
	}
	var links, nodes int
	if err := database.QueryRow(`SELECT COUNT(*) FROM application_service_resources WHERE service_id = ?`, created.ID).Scan(&links); err != nil {
		t.Fatalf("count deleted links: %v", err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM nodes WHERE id = 'original-node'`).Scan(&nodes); err != nil {
		t.Fatalf("count original nodes: %v", err)
	}
	if links != 0 || nodes != 1 {
		t.Fatalf("delete result links=%d nodes=%d, want 0/1", links, nodes)
	}
	if err := store.Delete(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete error = %v, want ErrNotFound", err)
	}
	if _, err := store.Get(ctx, other.ID); err != nil {
		t.Fatalf("delete affected unrelated service: %v", err)
	}
}

func TestStoreRejectsInvalidAndDuplicateResources(t *testing.T) {
	store, _ := newServiceCenterTestStore(t)
	tests := []struct {
		name  string
		input ServiceInput
		want  error
	}{
		{name: "empty name", input: ServiceInput{Name: "   "}, want: ErrInvalid},
		{name: "unknown resource", input: ServiceInput{Name: "x", Resources: []Resource{{ResourceType: "secret", ResourceKey: "one"}}}, want: ErrInvalid},
		{name: "invalid k8s kind", input: ServiceInput{Name: "x", Resources: []Resource{{ResourceType: ResourceK8sWorkload, ScopeID: "cluster", ResourceKind: "pod", Namespace: "default", ResourceKey: "one"}}}, want: ErrInvalid},
		{name: "invalid database id", input: ServiceInput{Name: "x", Resources: []Resource{{ResourceType: ResourceAlertRule, ResourceKey: "0"}}}, want: ErrInvalid},
		{name: "invalid managed id", input: ServiceInput{Name: "x", Resources: []Resource{{ResourceType: ResourceComposeProject, ScopeID: "node", ResourceKind: "managed", ResourceKey: "not-a-uuid"}}}, want: ErrInvalid},
		{name: "oversized namespace", input: ServiceInput{Name: "x", Resources: []Resource{{ResourceType: ResourceK8sWorkload, ScopeID: "cluster", ResourceKind: "deployment", Namespace: strings.Repeat("n", 192), ResourceKey: "api"}}}, want: ErrInvalid},
		{name: "duplicate association", input: ServiceInput{Name: "x", Resources: []Resource{
			{ResourceType: ResourceK8sWorkload, ScopeID: "cluster", ResourceKind: "DEPLOYMENT", Namespace: "default", ResourceKey: "api"},
			{ResourceType: ResourceK8sWorkload, ScopeID: "cluster", ResourceKind: "deployment", Namespace: "default", ResourceKey: "api"},
		}}, want: ErrConflict},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := store.Create(t.Context(), test.input)
			if !errors.Is(err, test.want) {
				t.Fatalf("Create error = %v, want %v", err, test.want)
			}
		})
	}
	if _, err := store.Update(t.Context(), "missing", ServiceInput{Name: "valid"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update missing error = %v, want ErrNotFound", err)
	}
}
