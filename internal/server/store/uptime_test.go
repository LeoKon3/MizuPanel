package store

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	serverdb "github.com/mizupanel/mizupanel/internal/server/db"
)

func testUptimeStore(t *testing.T) *UptimeStore {
	t.Helper()
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { database.Close() })
	if err := serverdb.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return NewUptimeStore(database)
}

func newTestUptimeMonitor() UptimeMonitor {
	return UptimeMonitor{
		Name:                   "Website",
		Type:                   "http",
		Target:                 "https://example.com/health",
		Enabled:                true,
		IntervalSeconds:        60,
		TimeoutSeconds:         5,
		FailureThreshold:       2,
		ExpectedStatusMin:      200,
		ExpectedStatusMax:      399,
		TLSExpiryThresholdDays: 30,
		NotificationChannels:   []NotificationChannel{},
	}
}

func TestUptimeStoreCRUDReturnsTypedEmptyCollections(t *testing.T) {
	ctx := context.Background()
	store := testUptimeStore(t)
	monitors, err := store.ListMonitors(ctx)
	if err != nil {
		t.Fatalf("list empty monitors: %v", err)
	}
	if monitors == nil || len(monitors) != 0 {
		t.Fatalf("empty monitors = %#v, want non-nil empty slice", monitors)
	}

	monitor := newTestUptimeMonitor()
	if err := store.CreateMonitor(ctx, &monitor); err != nil {
		t.Fatalf("create monitor: %v", err)
	}
	if monitor.ID == 0 || monitor.Status != UptimeStatusPending || monitor.NotificationChannels == nil {
		t.Fatalf("created monitor = %+v", monitor)
	}
	loaded, err := store.GetMonitor(ctx, monitor.ID)
	if err != nil {
		t.Fatalf("get monitor: %v", err)
	}
	if loaded == nil || loaded.Target != monitor.Target {
		t.Fatalf("loaded monitor = %+v", loaded)
	}
	results, err := store.ListResults(ctx, monitor.ID, 10)
	if err != nil {
		t.Fatalf("list empty results: %v", err)
	}
	incidents, err := store.ListIncidents(ctx, monitor.ID, 10)
	if err != nil {
		t.Fatalf("list empty incidents: %v", err)
	}
	if results == nil || incidents == nil || len(results) != 0 || len(incidents) != 0 {
		t.Fatalf("empty collections results=%#v incidents=%#v", results, incidents)
	}
	deleted, err := store.DeleteMonitor(ctx, monitor.ID)
	if err != nil || !deleted {
		t.Fatalf("delete monitor: deleted=%v err=%v", deleted, err)
	}
	missing, err := store.GetMonitor(ctx, monitor.ID)
	if err != nil || missing != nil {
		t.Fatalf("missing monitor = %+v, err=%v", missing, err)
	}
}

func TestUptimeStoreAvailabilityIncidentThresholdAndRecovery(t *testing.T) {
	ctx := context.Background()
	store := testUptimeStore(t)
	monitor := newTestUptimeMonitor()
	if err := store.CreateMonitor(ctx, &monitor); err != nil {
		t.Fatalf("create monitor: %v", err)
	}
	base := time.Date(2026, 7, 25, 8, 0, 0, 0, time.UTC)
	first, err := store.ApplyProbe(ctx, monitor.ID, UptimeProbeResult{Error: "连接超时", LatencyMS: 5000, CheckedAt: base})
	if err != nil {
		t.Fatalf("first failure: %v", err)
	}
	if first.Monitor.Status != UptimeStatusDown || first.Monitor.ConsecutiveFailures != 1 || len(first.Triggered) != 0 {
		t.Fatalf("first transition = %+v", first)
	}
	second, err := store.ApplyProbe(ctx, monitor.ID, UptimeProbeResult{Error: "连接超时", LatencyMS: 5000, CheckedAt: base.Add(time.Minute)})
	if err != nil {
		t.Fatalf("second failure: %v", err)
	}
	if len(second.Triggered) != 1 || second.Triggered[0].Kind != UptimeIncidentAvailability {
		t.Fatalf("second triggered = %+v", second.Triggered)
	}
	third, err := store.ApplyProbe(ctx, monitor.ID, UptimeProbeResult{Error: "连接超时", LatencyMS: 5000, CheckedAt: base.Add(2 * time.Minute)})
	if err != nil {
		t.Fatalf("third failure: %v", err)
	}
	if len(third.Triggered) != 0 {
		t.Fatalf("third triggered duplicate incidents: %+v", third.Triggered)
	}
	recovery, err := store.ApplyProbe(ctx, monitor.ID, UptimeProbeResult{Success: true, LatencyMS: 25, StatusCode: 204, CheckedAt: base.Add(3 * time.Minute)})
	if err != nil {
		t.Fatalf("recovery: %v", err)
	}
	if recovery.Monitor.Status != UptimeStatusUp || recovery.Monitor.ConsecutiveFailures != 0 || len(recovery.Resolved) != 1 {
		t.Fatalf("recovery transition = %+v", recovery)
	}
	incidents, err := store.ListIncidents(ctx, monitor.ID, 10)
	if err != nil {
		t.Fatalf("list incidents: %v", err)
	}
	if len(incidents) != 1 || incidents[0].ResolvedAt == nil {
		t.Fatalf("incidents = %+v", incidents)
	}
}

func TestUptimeStoreEnforcesSingleActiveIncidentPerKind(t *testing.T) {
	ctx := context.Background()
	store := testUptimeStore(t)
	monitor := newTestUptimeMonitor()
	monitor.FailureThreshold = 1
	if err := store.CreateMonitor(ctx, &monitor); err != nil {
		t.Fatalf("create monitor: %v", err)
	}
	base := time.Date(2026, 7, 25, 8, 0, 0, 0, time.UTC)
	triggered, err := store.ApplyProbe(ctx, monitor.ID, UptimeProbeResult{Error: "连接失败", CheckedAt: base})
	if err != nil || len(triggered.Triggered) != 1 {
		t.Fatalf("trigger incident: transition=%+v err=%v", triggered, err)
	}
	incidentID := triggered.Triggered[0].ID
	var activeMarker sql.NullInt64
	if err := store.db.QueryRowContext(ctx, `SELECT active_marker FROM uptime_incidents WHERE id = ?`, incidentID).Scan(&activeMarker); err != nil {
		t.Fatalf("query active marker: %v", err)
	}
	if !activeMarker.Valid || activeMarker.Int64 != 1 {
		t.Fatalf("active marker = %+v, want 1", activeMarker)
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO uptime_incidents (monitor_id, kind, message, started_at, active_marker, created_at) VALUES (?, ?, ?, ?, 1, ?)`, monitor.ID, UptimeIncidentAvailability, "duplicate", formatTime(base.Add(time.Second)), formatTime(base.Add(time.Second))); err == nil {
		t.Fatal("inserted a second active availability incident")
	}

	recovered, err := store.ApplyProbe(ctx, monitor.ID, UptimeProbeResult{Success: true, StatusCode: 200, CheckedAt: base.Add(time.Minute)})
	if err != nil || len(recovered.Resolved) != 1 {
		t.Fatalf("resolve incident: transition=%+v err=%v", recovered, err)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT active_marker FROM uptime_incidents WHERE id = ?`, incidentID).Scan(&activeMarker); err != nil {
		t.Fatalf("query resolved marker: %v", err)
	}
	if activeMarker.Valid {
		t.Fatalf("resolved active marker = %+v, want NULL", activeMarker)
	}

	retriggered, err := store.ApplyProbe(ctx, monitor.ID, UptimeProbeResult{Error: "再次失败", CheckedAt: base.Add(2 * time.Minute)})
	if err != nil || len(retriggered.Triggered) != 1 {
		t.Fatalf("retrigger incident: transition=%+v err=%v", retriggered, err)
	}
	var activeCount int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM uptime_incidents WHERE monitor_id = ? AND kind = ? AND active_marker = 1`, monitor.ID, UptimeIncidentAvailability).Scan(&activeCount); err != nil {
		t.Fatalf("count active incidents: %v", err)
	}
	if activeCount != 1 {
		t.Fatalf("active incidents = %d, want 1", activeCount)
	}
}

func TestUptimeStoreCertificateWarningAndRenewal(t *testing.T) {
	ctx := context.Background()
	store := testUptimeStore(t)
	monitor := newTestUptimeMonitor()
	if err := store.CreateMonitor(ctx, &monitor); err != nil {
		t.Fatalf("create monitor: %v", err)
	}
	base := time.Date(2026, 7, 25, 8, 0, 0, 0, time.UTC)
	expiring := base.Add(10 * 24 * time.Hour)
	warning, err := store.ApplyProbe(ctx, monitor.ID, UptimeProbeResult{Success: true, LatencyMS: 20, StatusCode: 200, TLSChecked: true, TLSExpiring: true, TLSExpiresAt: &expiring, CheckedAt: base})
	if err != nil {
		t.Fatalf("certificate warning: %v", err)
	}
	if warning.Monitor.Status != UptimeStatusWarning || len(warning.Triggered) != 1 || warning.Triggered[0].Kind != UptimeIncidentCertificate {
		t.Fatalf("warning transition = %+v", warning)
	}
	if warning.Monitor.TLSRemainingDays == nil || *warning.Monitor.TLSRemainingDays != 10 {
		t.Fatalf("warning remaining days = %v, want 10", warning.Monitor.TLSRemainingDays)
	}
	renewed := base.Add(100 * 24 * time.Hour)
	recovery, err := store.ApplyProbe(ctx, monitor.ID, UptimeProbeResult{Success: true, LatencyMS: 18, StatusCode: 200, TLSChecked: true, TLSExpiresAt: &renewed, CheckedAt: base.Add(time.Hour)})
	if err != nil {
		t.Fatalf("certificate renewal: %v", err)
	}
	if recovery.Monitor.Status != UptimeStatusUp || len(recovery.Resolved) != 1 || recovery.Resolved[0].Kind != UptimeIncidentCertificate {
		t.Fatalf("renewal transition = %+v", recovery)
	}
	if recovery.Monitor.TLSRemainingDays == nil || *recovery.Monitor.TLSRemainingDays != 100 {
		t.Fatalf("renewal remaining days = %v, want 100", recovery.Monitor.TLSRemainingDays)
	}
}

func TestUptimeStoreMaterialUpdateResetsProjectionAndClosesIncident(t *testing.T) {
	ctx := context.Background()
	store := testUptimeStore(t)
	monitor := newTestUptimeMonitor()
	monitor.FailureThreshold = 1
	if err := store.CreateMonitor(ctx, &monitor); err != nil {
		t.Fatalf("create monitor: %v", err)
	}
	checkedAt := time.Date(2026, 7, 25, 8, 0, 0, 0, time.UTC)
	transition, err := store.ApplyProbe(ctx, monitor.ID, UptimeProbeResult{Error: "连接失败", CheckedAt: checkedAt})
	if err != nil || len(transition.Triggered) != 1 {
		t.Fatalf("trigger incident: transition=%+v err=%v", transition, err)
	}
	monitor.Target = "https://example.net/health"
	material, err := store.UpdateMonitor(ctx, &monitor)
	if err != nil {
		t.Fatalf("update monitor: %v", err)
	}
	if !material || monitor.Status != UptimeStatusPending || monitor.LastCheckedAt != nil || monitor.ConsecutiveFailures != 0 {
		t.Fatalf("updated monitor = %+v material=%v", monitor, material)
	}
	incidents, err := store.ListIncidents(ctx, monitor.ID, 10)
	if err != nil {
		t.Fatalf("list incidents: %v", err)
	}
	if len(incidents) != 1 || incidents[0].ResolvedAt == nil || incidents[0].RecoveryNotificationAttemptedAt != nil {
		t.Fatalf("closed incidents = %+v", incidents)
	}
	var activeMarker sql.NullInt64
	if err := store.db.QueryRowContext(ctx, `SELECT active_marker FROM uptime_incidents WHERE id = ?`, incidents[0].ID).Scan(&activeMarker); err != nil {
		t.Fatalf("query closed incident marker: %v", err)
	}
	if activeMarker.Valid {
		t.Fatalf("closed incident marker = %+v, want NULL", activeMarker)
	}
}

func TestUptimeStoreRetainsOnlyNewestResults(t *testing.T) {
	ctx := context.Background()
	store := testUptimeStore(t)
	monitor := newTestUptimeMonitor()
	if err := store.CreateMonitor(ctx, &monitor); err != nil {
		t.Fatalf("create monitor: %v", err)
	}
	base := time.Date(2026, 7, 25, 8, 0, 0, 0, time.UTC)
	for index := 0; index < MaxUptimeResults+5; index++ {
		if _, err := store.ApplyProbe(ctx, monitor.ID, UptimeProbeResult{Success: true, LatencyMS: int64(index), StatusCode: 200, CheckedAt: base.Add(time.Duration(index) * time.Second)}); err != nil {
			t.Fatalf("apply result %d: %v", index, err)
		}
	}
	results, err := store.ListResults(ctx, monitor.ID, MaxUptimeResults+50)
	if err != nil {
		t.Fatalf("list results: %v", err)
	}
	if len(results) != MaxUptimeResults {
		t.Fatalf("results = %d, want %d", len(results), MaxUptimeResults)
	}
	if results[0].LatencyMS != MaxUptimeResults+4 || results[len(results)-1].LatencyMS != 5 {
		t.Fatalf("retained latency range = %d..%d", results[0].LatencyMS, results[len(results)-1].LatencyMS)
	}
}

func TestUptimeStorePersistsNotificationDeliveryResults(t *testing.T) {
	ctx := context.Background()
	store := testUptimeStore(t)
	monitor := newTestUptimeMonitor()
	monitor.FailureThreshold = 1
	if err := store.CreateMonitor(ctx, &monitor); err != nil {
		t.Fatalf("create monitor: %v", err)
	}
	checkedAt := time.Date(2026, 7, 25, 8, 0, 0, 0, time.UTC)
	transition, err := store.ApplyProbe(ctx, monitor.ID, UptimeProbeResult{Error: "连接失败", CheckedAt: checkedAt})
	if err != nil || len(transition.Triggered) != 1 {
		t.Fatalf("trigger: transition=%+v err=%v", transition, err)
	}
	incidentID := transition.Triggered[0].ID
	if err := store.UpdateIncidentNotification(ctx, incidentID, false, false, "webhook: request failed", checkedAt.Add(time.Second)); err != nil {
		t.Fatalf("update trigger delivery: %v", err)
	}
	recovery, err := store.ApplyProbe(ctx, monitor.ID, UptimeProbeResult{Success: true, StatusCode: 200, CheckedAt: checkedAt.Add(time.Minute)})
	if err != nil || len(recovery.Resolved) != 1 {
		t.Fatalf("resolve: transition=%+v err=%v", recovery, err)
	}
	if err := store.UpdateIncidentNotification(ctx, incidentID, true, true, "", checkedAt.Add(time.Minute+time.Second)); err != nil {
		t.Fatalf("update recovery delivery: %v", err)
	}
	incidents, err := store.ListIncidents(ctx, monitor.ID, 10)
	if err != nil {
		t.Fatalf("list incidents: %v", err)
	}
	if len(incidents) != 1 || incidents[0].NotificationSent || incidents[0].NotificationError == "" || !incidents[0].RecoveryNotificationSent {
		t.Fatalf("incident delivery = %+v", incidents)
	}
}
