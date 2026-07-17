package api

import (
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/mizupanel/mizupanel/internal/protocol"
	"github.com/mizupanel/mizupanel/internal/server/store"
)

func (s *Server) handleNodeConnectionDiagnostics(w http.ResponseWriter, r *http.Request, nodeID string) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if s.diagnostics == nil {
		writeError(w, http.StatusServiceUnavailable, "connection diagnostics unavailable")
		return
	}
	diagnostics, err := s.diagnostics.ConnectionDiagnostics(r.Context(), nodeID)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "node not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, diagnostics)
}

type NodeResponse struct {
	ID              string                  `json:"id"`
	Name            string                  `json:"name"`
	Hostname        string                  `json:"hostname"`
	IP              string                  `json:"ip"`
	OS              string                  `json:"os"`
	Arch            string                  `json:"arch"`
	Kernel          string                  `json:"kernel"`
	AgentVersion    string                  `json:"agent_version"`
	AgentMode       string                  `json:"agent_mode"`
	AgentUser       string                  `json:"agent_user"`
	Status          string                  `json:"status"`
	LastSeenAt      time.Time               `json:"last_seen_at"`
	TerminalEnabled bool                    `json:"terminal_enabled"`
	LatestMetric    *MetricResponse         `json:"latest_metric,omitempty"`
	Group           *store.NodeGroupSummary `json:"group"`
	Tags            []store.NodeTagSummary  `json:"tags"`
}

type MetricResponse struct {
	ID             int64     `json:"id"`
	NodeID         string    `json:"node_id"`
	CPUUsage       float64   `json:"cpu_usage"`
	CPUCores       int       `json:"cpu_cores"`
	MemoryTotal    int64     `json:"memory_total"`
	MemoryUsed     int64     `json:"memory_used"`
	MemoryUsage    float64   `json:"memory_usage"`
	DiskTotal      int64     `json:"disk_total"`
	DiskUsed       int64     `json:"disk_used"`
	DiskUsage      float64   `json:"disk_usage"`
	Uptime         int64     `json:"uptime"`
	DiskReadSpeed  int64     `json:"disk_read_speed"`
	DiskWriteSpeed int64     `json:"disk_write_speed"`
	RXSpeed        int64     `json:"rx_speed"`
	TXSpeed        int64     `json:"tx_speed"`
	RXTotal        int64     `json:"rx_total"`
	TXTotal        int64     `json:"tx_total"`
	Load1          float64   `json:"load1"`
	Load5          float64   `json:"load5"`
	Load15         float64   `json:"load15"`
	CreatedAt      time.Time `json:"created_at"`
}

type ProcessSnapshotResponse struct {
	NodeID      string                 `json:"node_id"`
	CollectedAt int64                  `json:"collected_at"`
	Error       string                 `json:"error"`
	Processes   []protocol.ProcessInfo `json:"processes"`
}

type DockerSnapshotResponse struct {
	NodeID      string                   `json:"node_id"`
	CollectedAt int64                    `json:"collected_at"`
	Available   bool                     `json:"available"`
	Version     string                   `json:"version,omitempty"`
	Error       string                   `json:"error"`
	Containers  []protocol.ContainerInfo `json:"containers"`
}

func nodeResponse(node store.Node) NodeResponse {
	return NodeResponse{
		ID:              node.ID,
		Name:            node.Name,
		Hostname:        node.Hostname,
		IP:              node.IP,
		OS:              node.OS,
		Arch:            node.Arch,
		Kernel:          node.Kernel,
		AgentVersion:    node.AgentVersion,
		AgentMode:       node.AgentMode,
		AgentUser:       node.AgentUser,
		Status:          node.Status,
		LastSeenAt:      node.LastSeenAt,
		TerminalEnabled: node.TerminalEnabled,
		Tags:            make([]store.NodeTagSummary, 0),
	}
}

func metricResponse(metric store.Metric) MetricResponse {
	return MetricResponse{
		ID:             metric.ID,
		NodeID:         metric.NodeID,
		CPUUsage:       metric.CPUUsage,
		CPUCores:       metric.CPUCores,
		MemoryTotal:    metric.MemoryTotal,
		MemoryUsed:     metric.MemoryUsed,
		MemoryUsage:    metric.MemoryUsage,
		DiskTotal:      metric.DiskTotal,
		DiskUsed:       metric.DiskUsed,
		DiskUsage:      metric.DiskUsage,
		Uptime:         metric.Uptime,
		DiskReadSpeed:  metric.DiskReadSpeed,
		DiskWriteSpeed: metric.DiskWriteSpeed,
		RXSpeed:        metric.RXSpeed,
		TXSpeed:        metric.TXSpeed,
		RXTotal:        metric.RXTotal,
		TXTotal:        metric.TXTotal,
		Load1:          metric.Load1,
		Load5:          metric.Load5,
		Load15:         metric.Load15,
		CreatedAt:      metric.CreatedAt,
	}
}
