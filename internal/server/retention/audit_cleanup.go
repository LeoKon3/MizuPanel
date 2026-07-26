package retention

import (
	"context"
	"time"
)

type AuditPruner interface {
	DeleteOlderThan(ctx context.Context, cutoff time.Time) (int64, error)
}

type AuditCleaner struct {
	store     AuditPruner
	retention time.Duration
}

func NewAuditCleaner(store AuditPruner, retention time.Duration) *AuditCleaner {
	return &AuditCleaner{store: store, retention: retention}
}

func (c *AuditCleaner) RunOnce(ctx context.Context, now time.Time) (int64, error) {
	return c.store.DeleteOlderThan(ctx, now.UTC().Add(-c.retention))
}

func (c *AuditCleaner) Run(ctx context.Context, interval time.Duration) {
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
