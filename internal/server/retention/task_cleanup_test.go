package retention

import (
	"context"
	"testing"
	"time"
)

type recordingTaskRunPruner struct {
	cutoff time.Time
}

func (p *recordingTaskRunPruner) DeleteRunsOlderThan(_ context.Context, cutoff time.Time) (int64, error) {
	p.cutoff = cutoff
	return 4, nil
}

func TestTaskRunCleanerUsesUTCRetentionCutoff(t *testing.T) {
	pruner := &recordingTaskRunPruner{}
	cleaner := NewTaskRunCleaner(pruner, 30*24*time.Hour)
	now := time.Date(2026, 7, 26, 18, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	deleted, err := cleaner.RunOnce(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 4 {
		t.Fatalf("deleted = %d, want 4", deleted)
	}
	want := now.UTC().Add(-30 * 24 * time.Hour)
	if !pruner.cutoff.Equal(want) || pruner.cutoff.Location() != time.UTC {
		t.Fatalf("cutoff = %s (%s), want %s UTC", pruner.cutoff, pruner.cutoff.Location(), want)
	}
}
