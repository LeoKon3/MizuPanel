package retention

import (
	"context"
	"errors"
	"testing"
	"time"
)

type auditPrunerStub struct {
	cutoff time.Time
	count  int64
	err    error
}

func (s *auditPrunerStub) DeleteOlderThan(_ context.Context, cutoff time.Time) (int64, error) {
	s.cutoff = cutoff
	return s.count, s.err
}

func TestAuditCleanerUsesUTCDeterministicCutoff(t *testing.T) {
	stub := &auditPrunerStub{count: 7}
	cleaner := NewAuditCleaner(stub, 90*24*time.Hour)
	now := time.Date(2026, 7, 26, 16, 0, 0, 0, time.FixedZone("test", 8*60*60))
	count, err := cleaner.RunOnce(context.Background(), now)
	if err != nil || count != 7 {
		t.Fatalf("RunOnce = %d, %v", count, err)
	}
	want := now.UTC().Add(-90 * 24 * time.Hour)
	if !stub.cutoff.Equal(want) || stub.cutoff.Location() != time.UTC {
		t.Fatalf("cutoff = %s (%s), want %s UTC", stub.cutoff, stub.cutoff.Location(), want)
	}
}

func TestAuditCleanerPropagatesStoreError(t *testing.T) {
	want := errors.New("store unavailable")
	cleaner := NewAuditCleaner(&auditPrunerStub{err: want}, 24*time.Hour)
	if _, err := cleaner.RunOnce(context.Background(), time.Now()); !errors.Is(err, want) {
		t.Fatalf("RunOnce error = %v", err)
	}
}
