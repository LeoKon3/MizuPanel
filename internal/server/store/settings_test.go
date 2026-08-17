package store

import (
	"reflect"
	"testing"
	"time"
)

func TestSettingsStorePersistsMetricsRetention(t *testing.T) {
	db := openTestDB(t)
	settings := NewSettingsStore(db)

	if got, err := settings.MetricsRetention(t.Context(), 6*time.Hour); err != nil || got != 6*time.Hour {
		t.Fatalf("default retention = %s, %v; want 6h, nil", got, err)
	}
	if err := settings.SetMetricsRetention(t.Context(), "3d"); err != nil {
		t.Fatalf("set retention: %v", err)
	}
	got, err := settings.MetricsRetention(t.Context(), 6*time.Hour)
	if err != nil {
		t.Fatalf("get retention: %v", err)
	}
	if got != 72*time.Hour {
		t.Fatalf("retention = %s, want 72h", got)
	}
	value, err := settings.MetricsRetentionValue(t.Context(), 6*time.Hour)
	if err != nil {
		t.Fatalf("get retention value: %v", err)
	}
	if value != "3d" {
		t.Fatalf("retention value = %q, want 3d", value)
	}
}

func TestSettingsStoreRejectsMetricsRetentionOverSevenDays(t *testing.T) {
	db := openTestDB(t)
	settings := NewSettingsStore(db)

	if err := settings.SetMetricsRetention(t.Context(), "8d"); err == nil {
		t.Fatal("set 8d returned nil, want error")
	}
}

func TestSettingsStorePersistsAIControlPolicyWithRevision(t *testing.T) {
	db := openTestDB(t)
	settings := NewSettingsStore(db)

	initial, err := settings.AIControlPolicy(t.Context())
	if err != nil {
		t.Fatalf("get default policy: %v", err)
	}
	if initial.Mode != AIControlConfirmAll || initial.Revision != 1 || len(initial.AllowedActions) != 0 || len(initial.NodeScope) != 0 {
		t.Fatalf("default policy = %+v", initial)
	}

	written, err := settings.SetAIControlPolicy(t.Context(), AIControlPolicy{
		Mode:           AIControlLowRiskAuto,
		AllowedActions: []string{AIControlActionDockerContainerRestart, AIControlActionSystemdServiceStart},
		NodeScope:      []string{"node-1"},
	})
	if err != nil {
		t.Fatalf("set policy: %v", err)
	}
	if written.Revision != 2 || written.UpdatedAt.IsZero() {
		t.Fatalf("written policy = %+v", written)
	}

	reloaded, err := NewSettingsStore(db).AIControlPolicy(t.Context())
	if err != nil {
		t.Fatalf("reload policy: %v", err)
	}
	if reloaded.Mode != written.Mode || reloaded.Revision != written.Revision ||
		!reflect.DeepEqual(reloaded.AllowedActions, written.AllowedActions) || !reflect.DeepEqual(reloaded.NodeScope, written.NodeScope) {
		t.Fatalf("reloaded policy = %+v, want %+v", reloaded, written)
	}
}

func TestSettingsStoreRejectsUnsafeAIControlPolicy(t *testing.T) {
	db := openTestDB(t)
	settings := NewSettingsStore(db)
	tests := []AIControlPolicy{
		{Mode: "automatic", Revision: 1},
		{Mode: AIControlLowRiskAuto, AllowedActions: []string{"node.reboot"}, Revision: 1},
		{Mode: AIControlLowRiskAuto, AllowedActions: []string{AIControlActionDockerContainerStart, AIControlActionDockerContainerStart}, Revision: 1},
		{Mode: AIControlLowRiskAuto, NodeScope: []string{"node-1", "node-1"}, Revision: 1},
	}
	for _, policy := range tests {
		if _, err := settings.SetAIControlPolicy(t.Context(), policy); err == nil {
			t.Fatalf("SetAIControlPolicy(%+v) returned nil", policy)
		}
	}
}
