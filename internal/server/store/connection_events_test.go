package store

import (
	"fmt"
	"testing"
	"time"
)

func TestConnectionEventStoreRetainsLatestTwentyAndSevenDays(t *testing.T) {
	db := openTestDB(t)
	nodes := NewNodeStore(db)
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	if err := nodes.Upsert(t.Context(), Node{ID: "node-1", Name: "node", Status: "online", LastSeenAt: now}); err != nil {
		t.Fatal(err)
	}
	events := NewConnectionEventStore(db)
	if err := events.Create(t.Context(), &ConnectionEvent{NodeID: "node-1", Type: ConnectionEventDisconnected, CreatedAt: now.Add(-8 * 24 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 25; i++ {
		if err := events.Create(t.Context(), &ConnectionEvent{NodeID: "node-1", Type: ConnectionEventConnected, Reason: fmt.Sprintf("event-%02d", i), CreatedAt: now.Add(time.Duration(i) * time.Minute)}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := events.List(t.Context(), "node-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 20 {
		t.Fatalf("len = %d, want 20", len(got))
	}
	if got[0].Reason != "event-24" || got[19].Reason != "event-05" {
		t.Fatalf("unexpected retained range: first=%q last=%q", got[0].Reason, got[19].Reason)
	}
}
