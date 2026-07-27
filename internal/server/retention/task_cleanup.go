package retention

import (
	"context"
	"time"
)

type TaskRunPruner interface {
	DeleteRunsOlderThan(ctx context.Context, cutoff time.Time) (int64, error)
}

type TaskRunCleaner struct {
	store     TaskRunPruner
	retention time.Duration
}

func NewTaskRunCleaner(store TaskRunPruner, retention time.Duration) *TaskRunCleaner {
	return &TaskRunCleaner{store: store, retention: retention}
}

func (c *TaskRunCleaner) RunOnce(ctx context.Context, now time.Time) (int64, error) {
	return c.store.DeleteRunsOlderThan(ctx, now.UTC().Add(-c.retention))
}

func (c *TaskRunCleaner) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			_, _ = c.RunOnce(ctx, now)
		}
	}
}
