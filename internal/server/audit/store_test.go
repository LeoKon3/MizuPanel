package audit

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	serverdb "github.com/mizupanel/mizupanel/internal/server/db"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	database, err := serverdb.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return NewStore(database, serverdb.DialectSQLite)
}

func testEvent(requestID, module, result string, createdAt time.Time) Event {
	return Event{
		RequestID:  requestID,
		CreatedAt:  createdAt,
		ActorType:  ActorAdmin,
		ActorName:  "admin",
		SourceIP:   "192.0.2.10",
		Module:     module,
		Action:     "update",
		TargetType: "node",
		TargetID:   "node-1",
		TargetName: "Web Node",
		NodeID:     "node-1",
		Result:     result,
		DurationMS: 12,
		Summary:    "completed",
		Metadata:   map[string]string{"enabled": "true"},
	}
}

func TestStoreCreateListFiltersAndKeyset(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	base := time.Date(2026, 7, 26, 8, 0, 0, 0, time.UTC)
	values := []Event{
		testEvent("request-1", "docker", ResultSuccess, base),
		testEvent("request-2", "uptime", ResultFailure, base.Add(time.Minute)),
		testEvent("request-3", "docker", ResultAccepted, base.Add(2*time.Minute)),
	}
	values[1].ActorType = ActorLocalAdmin
	values[1].ActorName = "local"
	values[1].NodeID = "node-2"
	values[1].TargetID = "monitor-7"
	values[1].TargetName = "Percent_100%"
	for index := range values {
		if err := store.Create(ctx, &values[index]); err != nil {
			t.Fatalf("Create(%d): %v", index, err)
		}
		if values[index].ID == 0 {
			t.Fatalf("Create(%d) did not assign ID", index)
		}
	}

	page, err := store.List(ctx, Filter{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 2 || page.Events[0].RequestID != "request-3" || page.NextBeforeID == nil {
		t.Fatalf("first page = %+v", page)
	}
	second, err := store.List(ctx, Filter{BeforeID: *page.NextBeforeID, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Events) != 1 || second.Events[0].RequestID != "request-1" || second.NextBeforeID != nil {
		t.Fatalf("second page = %+v", second)
	}

	from := base.Add(30 * time.Second)
	to := base.Add(90 * time.Second)
	filtered, err := store.List(ctx, Filter{
		From: &from, To: &to, ActorType: ActorLocalAdmin, ActorName: "local",
		Module: "uptime", Action: "update", NodeID: "node-2", Result: ResultFailure,
		Query: "percent_100%",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered.Events) != 1 || filtered.Events[0].Metadata["enabled"] != "true" {
		t.Fatalf("filtered = %+v", filtered.Events)
	}

	empty, err := store.List(ctx, Filter{Query: "missing"})
	if err != nil {
		t.Fatal(err)
	}
	if empty.Events == nil || len(empty.Events) != 0 || empty.NextBeforeID != nil {
		t.Fatalf("empty page = %#v", empty)
	}
}

func TestStoreDeleteOlderThanAndValidation(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	base := time.Date(2026, 7, 26, 8, 0, 0, 0, time.UTC)
	old := testEvent("old-request", "docker", ResultSuccess, base)
	recent := testEvent("new-request", "docker", ResultSuccess, base.Add(48*time.Hour))
	if err := store.Create(ctx, &old); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(ctx, &recent); err != nil {
		t.Fatal(err)
	}
	deleted, err := store.DeleteOlderThan(ctx, base.Add(24*time.Hour))
	if err != nil || deleted != 1 {
		t.Fatalf("DeleteOlderThan = %d, %v", deleted, err)
	}
	page, err := store.List(ctx, Filter{})
	if err != nil || len(page.Events) != 1 || page.Events[0].RequestID != "new-request" {
		t.Fatalf("remaining = %+v, %v", page.Events, err)
	}

	invalid := testEvent("request-invalid", "Docker", ResultSuccess, base)
	if err := store.Create(ctx, &invalid); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid module error = %v", err)
	}
	if _, err := store.List(ctx, Filter{Limit: MaxPageLimit + 1}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid limit error = %v", err)
	}
	if err := store.Create(ctx, nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("nil event error = %v", err)
	}

	invalidUTF8 := testEvent("request-invalid-utf8", "docker", ResultSuccess, base)
	invalidUTF8.TargetName = string([]byte{0xff})
	if err := store.Create(ctx, &invalidUTF8); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid UTF-8 event error = %v", err)
	}
	if _, err := store.List(ctx, Filter{Query: string([]byte{0xff})}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid UTF-8 filter error = %v", err)
	}
}

func TestStoreTimeFiltersUseLexicallyStableFractionalTimestamps(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	base := time.Date(2026, 7, 26, 8, 0, 0, 0, time.UTC)
	event := testEvent("fractional-request", "docker", ResultSuccess, base.Add(100*time.Millisecond))
	if err := store.Create(ctx, &event); err != nil {
		t.Fatal(err)
	}

	from := base
	to := base.Add(time.Second)
	page, err := store.List(ctx, Filter{From: &from, To: &to})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 1 || page.Events[0].RequestID != event.RequestID {
		t.Fatalf("fractional boundary page = %+v", page.Events)
	}
}

func TestStoreRejectsOutOfBoundsEventsAndFilters(t *testing.T) {
	store := newTestStore(t)
	base := time.Date(2026, 7, 26, 8, 0, 0, 0, time.UTC)
	eventCases := []struct {
		name   string
		mutate func(*Event)
	}{
		{name: "request ID", mutate: func(event *Event) { event.RequestID = strings.Repeat("r", maxRequestIDLength+1) }},
		{name: "actor name", mutate: func(event *Event) { event.ActorName = strings.Repeat("a", maxActorNameLength+1) }},
		{name: "source IP", mutate: func(event *Event) { event.SourceIP = strings.Repeat("1", maxSourceIPLength+1) }},
		{name: "target ID", mutate: func(event *Event) { event.TargetID = strings.Repeat("t", maxTargetIDLength+1) }},
		{name: "target name", mutate: func(event *Event) { event.TargetName = strings.Repeat("n", maxTargetNameLen+1) }},
		{name: "node ID", mutate: func(event *Event) { event.NodeID = strings.Repeat("n", maxNodeIDLength+1) }},
		{name: "negative duration", mutate: func(event *Event) { event.DurationMS = -1 }},
		{name: "summary", mutate: func(event *Event) { event.Summary = strings.Repeat("s", maxSummaryLength+1) }},
		{name: "metadata key", mutate: func(event *Event) {
			event.Metadata = map[string]string{strings.Repeat("k", maxMetadataKeyLen+1): "value"}
		}},
		{name: "metadata value", mutate: func(event *Event) { event.Metadata = map[string]string{"key": strings.Repeat("v", maxMetadataValue+1)} }},
		{name: "metadata count", mutate: func(event *Event) {
			event.Metadata = make(map[string]string, MaxMetadataItems+1)
			for index := 0; index <= MaxMetadataItems; index++ {
				event.Metadata["key_"+string(rune('a'+index))] = "value"
			}
		}},
	}
	for _, testCase := range eventCases {
		t.Run("event "+testCase.name, func(t *testing.T) {
			event := testEvent("request-bounds", "docker", ResultSuccess, base)
			testCase.mutate(&event)
			if err := store.Create(t.Context(), &event); !errors.Is(err, ErrInvalid) {
				t.Fatalf("Create error=%v, want ErrInvalid", err)
			}
		})
	}

	filterCases := []struct {
		name   string
		filter Filter
	}{
		{name: "negative cursor", filter: Filter{BeforeID: -1}},
		{name: "page limit", filter: Filter{Limit: MaxPageLimit + 1}},
		{name: "actor name", filter: Filter{ActorName: strings.Repeat("a", maxActorNameLength+1)}},
		{name: "node ID", filter: Filter{NodeID: strings.Repeat("n", maxNodeIDLength+1)}},
		{name: "query", filter: Filter{Query: strings.Repeat("q", MaxSearchLength+1)}},
		{name: "module", filter: Filter{Module: "Docker"}},
		{name: "action", filter: Filter{Action: "Restart"}},
		{name: "result", filter: Filter{Result: "pending"}},
	}
	for _, testCase := range filterCases {
		t.Run("filter "+testCase.name, func(t *testing.T) {
			if _, err := store.List(t.Context(), testCase.filter); !errors.Is(err, ErrInvalid) {
				t.Fatalf("List error=%v, want ErrInvalid", err)
			}
		})
	}
}
