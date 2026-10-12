package k8s

import "time"

const (
	ClusterStatusOnline        = "online"
	ClusterStatusOffline       = "offline"
	ClusterStatusUnknown       = "unknown"
	HealthReasonChecking       = "checking"
	HealthReasonStale          = "stale"
	HealthReasonAgentOffline   = "agent_offline"
	HealthReasonConfiguration  = "configuration_missing"
	HealthReasonAPIUnreachable = "api_unreachable"
)

const HealthFreshnessTTL = 90 * time.Second

func EffectiveClusterStatus(storedStatus, storedReason, nodeStatus string, checkedAt, now time.Time) (string, string) {
	if nodeStatus != "" && nodeStatus != ClusterStatusOnline {
		return ClusterStatusOffline, HealthReasonAgentOffline
	}
	if checkedAt.IsZero() {
		return ClusterStatusUnknown, HealthReasonChecking
	}
	if now.Sub(checkedAt) > HealthFreshnessTTL {
		return ClusterStatusUnknown, HealthReasonStale
	}
	switch storedStatus {
	case ClusterStatusOnline:
		return ClusterStatusOnline, storedReason
	case ClusterStatusOffline:
		if storedReason == "" {
			storedReason = HealthReasonAPIUnreachable
		}
		return ClusterStatusOffline, storedReason
	default:
		return ClusterStatusUnknown, HealthReasonChecking
	}
}

// Cluster K8s 集群
type Cluster struct {
	ID                string    `json:"id"`
	Name              string    `json:"name"`
	NodeID            string    `json:"node_id"`
	KubeconfigPath    string    `json:"kubeconfig_path,omitempty"`
	KubeconfigContent string    `json:"-"`
	Context           string    `json:"context,omitempty"`
	Status            string    `json:"status"` // online, offline, unknown
	HealthReason      string    `json:"health_reason,omitempty"`
	HealthCheckedAt   time.Time `json:"health_checked_at,omitempty"`
	Version           string    `json:"version,omitempty"`
	NodeCount         int       `json:"node_count,omitempty"`
	NamespaceCount    int       `json:"namespace_count,omitempty"`
	LastSeenAt        time.Time `json:"last_seen_at,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// PublicCluster is the API-safe K8s cluster representation.
type PublicCluster struct {
	ID                string     `json:"id"`
	Name              string     `json:"name"`
	NodeID            string     `json:"node_id"`
	KubeconfigPath    string     `json:"kubeconfig_path,omitempty"`
	KubeconfigContent string     `json:"-"`
	Context           string     `json:"context,omitempty"`
	Status            string     `json:"status"`
	HealthReason      string     `json:"health_reason,omitempty"`
	HealthCheckedAt   *time.Time `json:"health_checked_at,omitempty"`
	Version           string     `json:"version,omitempty"`
	NodeCount         int        `json:"node_count,omitempty"`
	NamespaceCount    int        `json:"namespace_count,omitempty"`
	LastSeenAt        time.Time  `json:"last_seen_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

func (c *Cluster) Public() PublicCluster {
	return PublicCluster{
		ID:              c.ID,
		Name:            c.Name,
		NodeID:          c.NodeID,
		KubeconfigPath:  c.KubeconfigPath,
		Context:         c.Context,
		Status:          c.Status,
		HealthReason:    c.HealthReason,
		HealthCheckedAt: optionalTime(c.HealthCheckedAt),
		Version:         c.Version,
		NodeCount:       c.NodeCount,
		NamespaceCount:  c.NamespaceCount,
		LastSeenAt:      c.LastSeenAt,
		CreatedAt:       c.CreatedAt,
		UpdatedAt:       c.UpdatedAt,
	}
}

func optionalTime(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	copy := value
	return &copy
}

func timeValue(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return *value
}

// PublicClusterWithNode 集群连同节点信息，不包含 kubeconfig 内容
type PublicClusterWithNode struct {
	PublicCluster
	NodeName       string    `json:"node_name"`
	NodeIP         string    `json:"node_ip"`
	NodeStatus     string    `json:"node_status"` // Agent 节点状态：online/offline
	NodeLastSeenAt time.Time `json:"node_last_seen_at,omitempty"`
}

// ClusterWithNode 集群连同节点信息
type ClusterWithNode struct {
	Cluster
	NodeName string `json:"node_name"`
	NodeIP   string `json:"node_ip"`
}

// ClusterInfo 集群详细信息
type ClusterInfo struct {
	Version        string `json:"version"`
	NodeCount      int    `json:"node_count"`
	NamespaceCount int    `json:"namespace_count"`
}
