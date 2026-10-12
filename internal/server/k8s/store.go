package k8s

import (
	"database/sql"
	"fmt"
	"time"
)

// Store K8s 集群存储层
type Store struct {
	db *sql.DB
}

// NewStore 创建存储层实例
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// CreateCluster 创建集群记录
func (s *Store) CreateCluster(cluster *Cluster) error {
	query := `INSERT INTO k8s_clusters (id, name, node_id, kubeconfig_path, kubeconfig_content, context, status, health_reason, health_checked_at, version, node_count, namespace_count, last_seen_at, created_at, updated_at)
          VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := s.db.Exec(query,
		cluster.ID,
		cluster.Name,
		cluster.NodeID,
		cluster.KubeconfigPath,
		cluster.KubeconfigContent,
		cluster.Context,
		cluster.Status,
		cluster.HealthReason,
		timeToString(cluster.HealthCheckedAt),
		cluster.Version,
		cluster.NodeCount,
		cluster.NamespaceCount,
		timeToString(cluster.LastSeenAt),
		timeToString(cluster.CreatedAt),
		timeToString(cluster.UpdatedAt),
	)
	return err
}

// GetCluster 获取集群记录
func (s *Store) GetCluster(id string) (*Cluster, error) {
	query := `SELECT id, name, node_id, kubeconfig_path, kubeconfig_content, context, status, health_reason, health_checked_at, version, node_count, namespace_count, last_seen_at, created_at, updated_at
	          FROM k8s_clusters WHERE id = ?`
	row := s.db.QueryRow(query, id)

	var cluster Cluster
	var healthCheckedAt, lastSeenAt, createdAt, updatedAt sql.NullString
	err := row.Scan(
		&cluster.ID,
		&cluster.Name,
		&cluster.NodeID,
		&cluster.KubeconfigPath,
		&cluster.KubeconfigContent,
		&cluster.Context,
		&cluster.Status,
		&cluster.HealthReason,
		&healthCheckedAt,
		&cluster.Version,
		&cluster.NodeCount,
		&cluster.NamespaceCount,
		&lastSeenAt,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("集群不存在")
		}
		return nil, err
	}

	cluster.LastSeenAt = stringToTime(lastSeenAt.String)
	cluster.HealthCheckedAt = stringToTime(healthCheckedAt.String)
	cluster.CreatedAt = stringToTime(createdAt.String)
	cluster.UpdatedAt = stringToTime(updatedAt.String)

	return &cluster, nil
}

// ListClusters 获取集群列表
func (s *Store) ListClusters() ([]*Cluster, error) {
	query := `SELECT id, name, node_id, kubeconfig_path, kubeconfig_content, context, status, health_reason, health_checked_at, version, node_count, namespace_count, last_seen_at, created_at, updated_at
	          FROM k8s_clusters ORDER BY created_at DESC`
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var clusters []*Cluster
	for rows.Next() {
		var cluster Cluster
		var healthCheckedAt, lastSeenAt, createdAt, updatedAt sql.NullString
		err := rows.Scan(
			&cluster.ID,
			&cluster.Name,
			&cluster.NodeID,
			&cluster.KubeconfigPath,
			&cluster.KubeconfigContent,
			&cluster.Context,
			&cluster.Status,
			&cluster.HealthReason,
			&healthCheckedAt,
			&cluster.Version,
			&cluster.NodeCount,
			&cluster.NamespaceCount,
			&lastSeenAt,
			&createdAt,
			&updatedAt,
		)
		if err != nil {
			return nil, err
		}

		cluster.LastSeenAt = stringToTime(lastSeenAt.String)
		cluster.HealthCheckedAt = stringToTime(healthCheckedAt.String)
		cluster.CreatedAt = stringToTime(createdAt.String)
		cluster.UpdatedAt = stringToTime(updatedAt.String)

		clusters = append(clusters, &cluster)
	}

	return clusters, nil
}

// ListClustersWithNodeInfo 获取集群列表（包含节点信息）
func (s *Store) ListClustersWithNodeInfo() ([]*PublicClusterWithNode, error) {
	query := `SELECT c.id, c.name, c.node_id, c.kubeconfig_path, c.context, c.status, c.health_reason, c.health_checked_at, c.version, c.node_count, c.namespace_count, c.last_seen_at, c.created_at, c.updated_at,
	                 COALESCE(n.name, '') as node_name, COALESCE(n.ip, '') as node_ip, COALESCE(n.status, 'offline') as node_status, COALESCE(n.last_seen_at, '') as node_last_seen_at
	          FROM k8s_clusters c
	          LEFT JOIN nodes n ON c.node_id = n.id
	          ORDER BY c.created_at DESC`
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var clusters []*PublicClusterWithNode
	for rows.Next() {
		cluster, err := scanPublicClusterWithNode(rows)
		if err != nil {
			return nil, err
		}

		clusters = append(clusters, cluster)
	}

	return clusters, nil
}

// GetClusterWithNodeInfo 获取单个集群详情（包含节点信息）
func (s *Store) GetClusterWithNodeInfo(id string) (*PublicClusterWithNode, error) {
	query := `SELECT c.id, c.name, c.node_id, c.kubeconfig_path, c.context, c.status, c.health_reason, c.health_checked_at, c.version, c.node_count, c.namespace_count, c.last_seen_at, c.created_at, c.updated_at,
	                 COALESCE(n.name, '') as node_name, COALESCE(n.ip, '') as node_ip, COALESCE(n.status, 'offline') as node_status, COALESCE(n.last_seen_at, '') as node_last_seen_at
	          FROM k8s_clusters c
	          LEFT JOIN nodes n ON c.node_id = n.id
	          WHERE c.id = ?`

	cluster, err := scanPublicClusterWithNode(s.db.QueryRow(query, id))
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("集群不存在")
		}
		return nil, err
	}
	return cluster, nil
}

type publicClusterWithNodeScanner interface {
	Scan(dest ...any) error
}

func scanPublicClusterWithNode(scanner publicClusterWithNodeScanner) (*PublicClusterWithNode, error) {
	var cluster PublicClusterWithNode
	var healthCheckedAt, lastSeenAt, createdAt, updatedAt, nodeLastSeenAt sql.NullString
	err := scanner.Scan(
		&cluster.ID,
		&cluster.Name,
		&cluster.NodeID,
		&cluster.KubeconfigPath,
		&cluster.Context,
		&cluster.Status,
		&cluster.HealthReason,
		&healthCheckedAt,
		&cluster.Version,
		&cluster.NodeCount,
		&cluster.NamespaceCount,
		&lastSeenAt,
		&createdAt,
		&updatedAt,
		&cluster.NodeName,
		&cluster.NodeIP,
		&cluster.NodeStatus,
		&nodeLastSeenAt,
	)
	if err != nil {
		return nil, err
	}

	cluster.LastSeenAt = stringToTime(lastSeenAt.String)
	cluster.HealthCheckedAt = optionalTime(stringToTime(healthCheckedAt.String))
	cluster.CreatedAt = stringToTime(createdAt.String)
	cluster.UpdatedAt = stringToTime(updatedAt.String)
	cluster.NodeLastSeenAt = stringToTime(nodeLastSeenAt.String)
	cluster.Status, cluster.HealthReason = EffectiveClusterStatus(cluster.Status, cluster.HealthReason, cluster.NodeStatus, timeValue(cluster.HealthCheckedAt), time.Now())

	return &cluster, nil
}

// UpdateClusterHealth stores one completed Kubernetes API health probe.
func (s *Store) UpdateClusterHealth(id, status, reason string, checkedAt time.Time, successful bool) error {
	now := time.Now().UTC()
	if successful {
		_, err := s.db.Exec(`UPDATE k8s_clusters SET status = ?, health_reason = ?, health_checked_at = ?, last_seen_at = ?, updated_at = ? WHERE id = ?`, status, reason, timeToString(checkedAt), timeToString(checkedAt), timeToString(now), id)
		return err
	}
	_, err := s.db.Exec(`UPDATE k8s_clusters SET status = ?, health_reason = ?, health_checked_at = ?, updated_at = ? WHERE id = ?`, status, reason, timeToString(checkedAt), timeToString(now), id)
	return err
}

// DeleteCluster 删除集群记录
func (s *Store) DeleteCluster(id string) error {
	query := `DELETE FROM k8s_clusters WHERE id = ?`
	_, err := s.db.Exec(query, id)
	return err
}

// timeToString 时间转字符串
func timeToString(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339Nano)
}

// stringToTime 字符串转时间
func stringToTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}
