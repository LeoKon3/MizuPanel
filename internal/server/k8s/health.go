package k8s

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"
)

const (
	DefaultHealthSweepInterval = 30 * time.Second
	DefaultHealthProbeTimeout  = 3 * time.Second
	DefaultHealthConcurrency   = 4
)

// HealthMonitor periodically probes configured Kubernetes APIs without making
// list or overview requests wait for remote network activity.
type HealthMonitor struct {
	service       *Service
	sweepInterval time.Duration
	probeTimeout  time.Duration
	semaphore     chan struct{}

	sweepMu sync.Mutex
	running bool

	generationMu sync.Mutex
	generation   map[string]uint64
}

func NewHealthMonitor(service *Service) *HealthMonitor {
	return &HealthMonitor{
		service:       service,
		sweepInterval: DefaultHealthSweepInterval,
		probeTimeout:  DefaultHealthProbeTimeout,
		semaphore:     make(chan struct{}, DefaultHealthConcurrency),
		generation:    make(map[string]uint64),
	}
}

func (m *HealthMonitor) SetSweepInterval(interval time.Duration) {
	if interval > 0 {
		m.sweepInterval = interval
	}
}

func (m *HealthMonitor) SetProbeTimeout(timeout time.Duration) {
	if timeout > 0 {
		m.probeTimeout = timeout
	}
}

// Run performs an immediate sweep and then continues on a fixed ticker.
func (m *HealthMonitor) Run(ctx context.Context) {
	m.runSweep(ctx)
	interval := m.sweepInterval
	if interval <= 0 {
		interval = DefaultHealthSweepInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.runSweep(ctx)
		}
	}
}

func (m *HealthMonitor) runSweep(ctx context.Context) {
	if err := m.Sweep(ctx); err != nil && ctx.Err() == nil {
		log.Printf("Kubernetes health sweep failed: %v", err)
	}
}

// Sweep checks every configured cluster. A concurrent sweep is skipped so a
// slow remote probe cannot create an unbounded backlog.
func (m *HealthMonitor) Sweep(ctx context.Context) error {
	if !m.beginSweep() {
		return nil
	}
	defer m.endSweep()

	clusters, err := m.service.store.ListClusters()
	if err != nil {
		return err
	}
	var waitGroup sync.WaitGroup
	for _, cluster := range clusters {
		if cluster == nil {
			continue
		}
		generation := m.nextGeneration(cluster.ID)
		if !m.service.hub.IsNodeOnline(cluster.NodeID) {
			m.commit(ctx, cluster.ID, generation, ClusterStatusOffline, HealthReasonAgentOffline, time.Time{}, false)
			continue
		}
		if strings.TrimSpace(cluster.KubeconfigContent) == "" {
			m.commit(ctx, cluster.ID, generation, ClusterStatusOffline, HealthReasonConfiguration, time.Now().UTC(), false)
			continue
		}

		waitGroup.Add(1)
		go func(clusterID string, generation uint64) {
			defer waitGroup.Done()
			if err := m.acquire(ctx); err != nil {
				return
			}
			defer m.release()

			probeCtx, cancel := context.WithTimeout(ctx, m.probeTimeout)
			_, probeErr := m.service.getSummary(probeCtx, clusterID, m.probeTimeout)
			cancel()
			if ctx.Err() != nil {
				return
			}
			if probeErr != nil {
				m.commit(ctx, clusterID, generation, ClusterStatusOffline, HealthReasonAPIUnreachable, time.Now().UTC(), false)
				return
			}
			m.commit(ctx, clusterID, generation, ClusterStatusOnline, "", time.Now().UTC(), true)
		}(cluster.ID, generation)
	}
	waitGroup.Wait()
	return ctx.Err()
}

func (m *HealthMonitor) beginSweep() bool {
	m.sweepMu.Lock()
	defer m.sweepMu.Unlock()
	if m.running {
		return false
	}
	m.running = true
	return true
}

func (m *HealthMonitor) endSweep() {
	m.sweepMu.Lock()
	m.running = false
	m.sweepMu.Unlock()
}

func (m *HealthMonitor) nextGeneration(clusterID string) uint64 {
	m.generationMu.Lock()
	defer m.generationMu.Unlock()
	m.generation[clusterID]++
	return m.generation[clusterID]
}

func (m *HealthMonitor) isLatest(clusterID string, generation uint64) bool {
	m.generationMu.Lock()
	defer m.generationMu.Unlock()
	return m.generation[clusterID] == generation
}

func (m *HealthMonitor) commit(ctx context.Context, clusterID string, generation uint64, status, reason string, checkedAt time.Time, successful bool) {
	if ctx.Err() != nil || !m.isLatest(clusterID, generation) {
		return
	}
	if err := m.service.store.UpdateClusterHealth(clusterID, status, reason, checkedAt, successful); err != nil && ctx.Err() == nil {
		log.Printf("Kubernetes health update failed cluster=%s: %v", clusterID, err)
	}
}

func (m *HealthMonitor) acquire(ctx context.Context) error {
	select {
	case m.semaphore <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *HealthMonitor) release() {
	<-m.semaphore
}
