package store

import (
	"context"
	"database/sql"
	"time"
)

const (
	ConnectionEventConnected        = "connected"
	ConnectionEventDisconnected     = "disconnected"
	ConnectionEventReplaced         = "replaced_by_new_connection"
	ConnectionEventIdentityConflict = "identity_conflict_suspected"
	ConnectionEventProtocolRejected = "protocol_rejected"
	ConnectionEventHeartbeatTimeout = "heartbeat_timeout"
	connectionEventRetention        = 7 * 24 * time.Hour
	connectionEventLimit            = 20
)

type ConnectionEvent struct {
	ID              int64     `json:"id"`
	NodeID          string    `json:"node_id"`
	Type            string    `json:"type"`
	Reason          string    `json:"reason,omitempty"`
	AgentVersion    string    `json:"agent_version,omitempty"`
	ProtocolVersion int       `json:"protocol_version"`
	IdentitySource  string    `json:"identity_source,omitempty"`
	Hostname        string    `json:"hostname,omitempty"`
	RemoteAddr      string    `json:"remote_addr,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
}

type ConnectionDiagnostics struct {
	NodeID               string            `json:"node_id"`
	Online               bool              `json:"online"`
	Health               string            `json:"health"`
	AgentVersion         string            `json:"agent_version,omitempty"`
	ProtocolVersion      int               `json:"protocol_version"`
	IdentitySource       string            `json:"identity_source,omitempty"`
	LastHeartbeatAt      *time.Time        `json:"last_heartbeat_at,omitempty"`
	LastConnectedAt      *time.Time        `json:"last_connected_at,omitempty"`
	LastDisconnectedAt   *time.Time        `json:"last_disconnected_at,omitempty"`
	LastDisconnectReason string            `json:"last_disconnect_reason,omitempty"`
	IdentityConflict     bool              `json:"identity_conflict"`
	UpgradeSupported     bool              `json:"upgrade_supported"`
	LatestVersion        string            `json:"latest_version,omitempty"`
	UpgradeAvailable     bool              `json:"upgrade_available"`
	Events               []ConnectionEvent `json:"events"`
}

type ConnectionEventStore struct{ db *sql.DB }

func NewConnectionEventStore(db *sql.DB) *ConnectionEventStore { return &ConnectionEventStore{db: db} }

func (s *ConnectionEventStore) Create(ctx context.Context, event *ConnectionEvent) error {
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO node_connection_events
		(node_id, event_type, reason, agent_version, protocol_version, identity_source, hostname, remote_addr, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, event.NodeID, event.Type, event.Reason, event.AgentVersion, event.ProtocolVersion, event.IdentitySource, event.Hostname, event.RemoteAddr, formatTime(event.CreatedAt))
	if err != nil {
		return err
	}
	event.ID, err = result.LastInsertId()
	if err != nil {
		return err
	}
	return s.Prune(ctx, event.NodeID, event.CreatedAt)
}

func (s *ConnectionEventStore) List(ctx context.Context, nodeID string) ([]ConnectionEvent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, node_id, event_type, reason, agent_version, protocol_version, identity_source, hostname, remote_addr, created_at
		FROM node_connection_events WHERE node_id = ? ORDER BY created_at DESC, id DESC LIMIT ?`, nodeID, connectionEventLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := make([]ConnectionEvent, 0)
	for rows.Next() {
		var event ConnectionEvent
		var created string
		if err := rows.Scan(&event.ID, &event.NodeID, &event.Type, &event.Reason, &event.AgentVersion, &event.ProtocolVersion, &event.IdentitySource, &event.Hostname, &event.RemoteAddr, &created); err != nil {
			return nil, err
		}
		event.CreatedAt, err = parseTime(created)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (s *ConnectionEventStore) Prune(ctx context.Context, nodeID string, now time.Time) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM node_connection_events WHERE node_id = ? AND created_at < ?`, nodeID, formatTime(now.Add(-connectionEventRetention))); err != nil {
		return err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM node_connection_events WHERE node_id = ? ORDER BY created_at DESC, id DESC LIMIT 1000 OFFSET ?`, nodeID, connectionEventLimit)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM node_connection_events WHERE id = ?`, id); err != nil {
			return err
		}
	}
	return nil
}
