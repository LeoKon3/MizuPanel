package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	serverdb "github.com/mizupanel/mizupanel/internal/server/db"
)

type Node struct {
	ID              string
	Name            string
	Hostname        string
	IP              string
	OS              string
	Arch            string
	Kernel          string
	AgentVersion    string
	AgentMode       string
	AgentUser       string
	Status          string
	TerminalEnabled bool // Agent 是否启用终端功能
	LastSeenAt      time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type NodeStore struct {
	db      *sql.DB
	dialect serverdb.Dialect
}

func NewNodeStore(db *sql.DB) *NodeStore {
	return NewNodeStoreWithDialect(db, serverdb.DialectSQLite)
}

func NewNodeStoreWithDialect(db *sql.DB, dialect serverdb.Dialect) *NodeStore {
	return &NodeStore{db: db, dialect: dialect}
}

// DB 返回底层数据库连接
func (s *NodeStore) DB() *sql.DB {
	return s.db
}

func (s *NodeStore) Dialect() serverdb.Dialect {
	return s.dialect
}

func (s *NodeStore) Upsert(ctx context.Context, node Node) error {
	now := time.Now().UTC()
	if node.CreatedAt.IsZero() {
		node.CreatedAt = now
	}
	if node.UpdatedAt.IsZero() {
		node.UpdatedAt = now
	}
	_, err := s.db.ExecContext(ctx, nodeUpsertSQL(s.dialect), node.ID, node.Name, node.Hostname, node.IP, node.OS, node.Arch, node.Kernel, node.AgentVersion, normalAgentMode(node.AgentMode), node.AgentUser, node.Status, node.TerminalEnabled, formatTime(node.LastSeenAt), formatTime(node.CreatedAt), formatTime(node.UpdatedAt))
	return err
}

func nodeUpsertSQL(dialect serverdb.Dialect) string {
	if dialect == serverdb.DialectMySQL {
		return `
				INSERT INTO nodes (id, name, hostname, ip, os, arch, kernel, agent_version, agent_mode, agent_user, status, terminal_enabled, last_seen_at, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				ON DUPLICATE KEY UPDATE
					name = VALUES(name),
					hostname = VALUES(hostname),
					ip = VALUES(ip),
					os = VALUES(os),
					arch = VALUES(arch),
					kernel = VALUES(kernel),
					agent_version = VALUES(agent_version),
					agent_mode = VALUES(agent_mode),
					agent_user = VALUES(agent_user),
					status = VALUES(status),
					terminal_enabled = VALUES(terminal_enabled),
					last_seen_at = VALUES(last_seen_at),
					updated_at = VALUES(updated_at)
			`
	}
	return `
				INSERT INTO nodes (id, name, hostname, ip, os, arch, kernel, agent_version, agent_mode, agent_user, status, terminal_enabled, last_seen_at, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT(id) DO UPDATE SET
					name = excluded.name,
					hostname = excluded.hostname,
					ip = excluded.ip,
					os = excluded.os,
					arch = excluded.arch,
					kernel = excluded.kernel,
					agent_version = excluded.agent_version,
					agent_mode = excluded.agent_mode,
					agent_user = excluded.agent_user,
					status = excluded.status,
					terminal_enabled = excluded.terminal_enabled,
					last_seen_at = excluded.last_seen_at,
					updated_at = excluded.updated_at
			`
}

func (s *NodeStore) Get(ctx context.Context, id string) (Node, error) {
	row := s.db.QueryRowContext(ctx, `
			SELECT id, name, hostname, ip, os, arch, kernel, agent_version, COALESCE(agent_mode, 'normal'), COALESCE(agent_user, ''), status, COALESCE(terminal_enabled, 0), last_seen_at, created_at, updated_at
			FROM nodes WHERE id = ?
		`, id)
	return scanNode(row)
}

func deletedNodeUpsertSQL(dialect serverdb.Dialect) string {
	if dialect == serverdb.DialectMySQL {
		return `INSERT INTO deleted_nodes (id, deleted_at) VALUES (?, ?) ON DUPLICATE KEY UPDATE deleted_at = VALUES(deleted_at)`
	}
	return `INSERT INTO deleted_nodes (id, deleted_at) VALUES (?, ?) ON CONFLICT(id) DO UPDATE SET deleted_at = excluded.deleted_at`
}

func (s *NodeStore) Delete(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var existingID string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM nodes WHERE id = ?`, id).Scan(&existingID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, deletedNodeUpsertSQL(s.dialect), id, formatTime(time.Now().UTC())); err != nil {
		return err
	}

	for _, statement := range []string{
		`DELETE FROM node_metrics WHERE node_id = ?`,
		`DELETE FROM node_tokens WHERE node_id = ?`,
		`DELETE FROM node_process_snapshots WHERE node_id = ?`,
		`DELETE FROM node_docker_snapshots WHERE node_id = ?`,
		`DELETE FROM node_tag_links WHERE node_id = ?`,
	} {
		if _, err := tx.ExecContext(ctx, statement, id); err != nil {
			return err
		}
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM nodes WHERE id = ?`, id)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}

func (s *NodeStore) DeleteIfDeleted(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var existingID string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM deleted_nodes WHERE id = ?`, id).Scan(&existingID); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}
	for _, statement := range []string{
		`DELETE FROM node_metrics WHERE node_id = ?`,
		`DELETE FROM node_tokens WHERE node_id = ?`,
		`DELETE FROM node_process_snapshots WHERE node_id = ?`,
		`DELETE FROM node_docker_snapshots WHERE node_id = ?`,
		`DELETE FROM node_tag_links WHERE node_id = ?`,
		`DELETE FROM nodes WHERE id = ?`,
	} {
		if _, err := tx.ExecContext(ctx, statement, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *NodeStore) Allow(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM deleted_nodes WHERE id = ?`, id)
	return err
}

func (s *NodeStore) AllowIfDeletedBefore(ctx context.Context, id string, cutoff time.Time) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	var deletedAtValue string
	err = tx.QueryRowContext(ctx, `SELECT deleted_at FROM deleted_nodes WHERE id = ?`, id).Scan(&deletedAtValue)
	if err == sql.ErrNoRows {
		return true, tx.Commit()
	}
	if err != nil {
		return false, err
	}
	deletedAt, err := parseTime(deletedAtValue)
	if err != nil {
		return false, err
	}
	if !deletedAt.Before(cutoff) {
		return false, nil
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM deleted_nodes WHERE id = ? AND deleted_at = ?`, id, deletedAtValue)
	if err != nil {
		return false, err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if rowsAffected == 0 {
		return false, nil
	}
	return true, tx.Commit()
}

func (s *NodeStore) IsDeleted(ctx context.Context, id string) (bool, error) {
	_, deleted, err := s.DeletedAt(ctx, id)
	return deleted, err
}

func (s *NodeStore) DeletedAt(ctx context.Context, id string) (time.Time, bool, error) {
	var deletedAt string
	err := s.db.QueryRowContext(ctx, `SELECT deleted_at FROM deleted_nodes WHERE id = ?`, id).Scan(&deletedAt)
	if err == sql.ErrNoRows {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	parsed, err := parseTime(deletedAt)
	if err != nil {
		return time.Time{}, false, err
	}
	return parsed, true, nil
}

func (s *NodeStore) SetStatus(ctx context.Context, id string, status string, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE nodes SET status = ?, updated_at = ? WHERE id = ?`, status, formatTime(now), id)
	return err
}

func (s *NodeStore) ResetOnlineStatuses(ctx context.Context, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE nodes SET status = 'offline', updated_at = ? WHERE status = 'online'`, formatTime(now))
	return err
}

func (s *NodeStore) UpdateSystemInfo(ctx context.Context, id string, hostname string, osName string, arch string, kernel string, now time.Time) error {
	result, err := s.db.ExecContext(ctx, `
			UPDATE nodes SET
				hostname = COALESCE(NULLIF(?, ''), hostname),
				os = COALESCE(NULLIF(?, ''), os),
				arch = COALESCE(NULLIF(?, ''), arch),
				kernel = COALESCE(NULLIF(?, ''), kernel),
				status = 'online',
				last_seen_at = ?,
				updated_at = ?
			WHERE id = ?
		`, hostname, osName, arch, kernel, formatTime(now), formatTime(now), id)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *NodeStore) List(ctx context.Context) ([]Node, error) {
	rows, err := s.db.QueryContext(ctx, `
			SELECT id, name, hostname, ip, os, arch, kernel, agent_version, COALESCE(agent_mode, 'normal'), COALESCE(agent_user, ''), status, COALESCE(terminal_enabled, 0), last_seen_at, created_at, updated_at
			FROM nodes ORDER BY name ASC
		`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []Node
	for rows.Next() {
		node, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}
	return nodes, rows.Err()
}

type nodeScanner interface {
	Scan(dest ...any) error
}

func scanNode(scanner nodeScanner) (Node, error) {
	var node Node
	var lastSeenAt, createdAt, updatedAt string
	if err := scanner.Scan(&node.ID, &node.Name, &node.Hostname, &node.IP, &node.OS, &node.Arch, &node.Kernel, &node.AgentVersion, &node.AgentMode, &node.AgentUser, &node.Status, &node.TerminalEnabled, &lastSeenAt, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Node{}, err
		}
		return Node{}, err
	}
	node.AgentMode = normalAgentMode(node.AgentMode)
	var err error
	if node.LastSeenAt, err = parseTime(lastSeenAt); err != nil {
		return Node{}, err
	}
	if node.CreatedAt, err = parseTime(createdAt); err != nil {
		return Node{}, err
	}
	if node.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return Node{}, err
	}
	return node, nil
}

func normalAgentMode(value string) string {
	if value == "ops" {
		return "ops"
	}
	return "normal"
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339Nano, value)
}
