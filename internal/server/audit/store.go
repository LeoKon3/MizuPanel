package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	serverdb "github.com/mizupanel/mizupanel/internal/server/db"
)

type Store struct {
	db      *sql.DB
	dialect serverdb.Dialect
}

const databaseTimeFormat = "2006-01-02T15:04:05.000000000Z"

func NewStore(db *sql.DB, dialect serverdb.Dialect) *Store {
	return &Store{db: db, dialect: dialect}
}

func (s *Store) Create(ctx context.Context, event *Event) error {
	if event == nil {
		return fmt.Errorf("%w: event", ErrInvalid)
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	} else {
		event.CreatedAt = event.CreatedAt.UTC()
	}
	if event.Metadata == nil {
		event.Metadata = map[string]string{}
	}
	if err := validateEvent(event); err != nil {
		return err
	}
	metadata, err := json.Marshal(event.Metadata)
	if err != nil {
		return fmt.Errorf("marshal audit metadata: %w", err)
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO audit_events
		(request_id, created_at, actor_type, actor_name, source_ip, module, action,
		 target_type, target_id, target_name, node_id, result, duration_ms, summary, metadata_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.RequestID, formatTime(event.CreatedAt), event.ActorType, event.ActorName,
		event.SourceIP, event.Module, event.Action, event.TargetType, event.TargetID,
		event.TargetName, event.NodeID, event.Result, event.DurationMS, event.Summary, string(metadata))
	if err != nil {
		return err
	}
	event.ID, err = result.LastInsertId()
	return err
}

func (s *Store) List(ctx context.Context, filter Filter) (Page, error) {
	if err := validateFilter(filter); err != nil {
		return Page{}, err
	}
	limit := filter.Limit
	if limit == 0 {
		limit = DefaultPageLimit
	}
	query := `SELECT id, request_id, created_at, actor_type, actor_name, source_ip,
		module, action, target_type, target_id, target_name, node_id, result,
		duration_ms, summary, metadata_json FROM audit_events WHERE 1 = 1`
	args := make([]any, 0, 14)
	add := func(clause string, value any) {
		query += clause
		args = append(args, value)
	}
	if filter.BeforeID > 0 {
		add(" AND id < ?", filter.BeforeID)
	}
	if filter.From != nil {
		add(" AND created_at >= ?", formatTime(filter.From.UTC()))
	}
	if filter.To != nil {
		add(" AND created_at <= ?", formatTime(filter.To.UTC()))
	}
	if filter.ActorType != "" {
		add(" AND actor_type = ?", filter.ActorType)
	}
	if filter.ActorName != "" {
		add(" AND actor_name = ?", filter.ActorName)
	}
	if filter.Module != "" {
		add(" AND module = ?", filter.Module)
	}
	if filter.Action != "" {
		add(" AND action = ?", filter.Action)
	}
	if filter.NodeID != "" {
		add(" AND node_id = ?", filter.NodeID)
	}
	if filter.Result != "" {
		add(" AND result = ?", filter.Result)
	}
	if filter.Query != "" {
		pattern := "%" + escapeLike(strings.ToLower(filter.Query)) + "%"
		query += ` AND (LOWER(target_id) LIKE ? ESCAPE '!' OR LOWER(target_name) LIKE ? ESCAPE '!'
			OR LOWER(summary) LIKE ? ESCAPE '!')`
		args = append(args, pattern, pattern, pattern)
	}
	query += " ORDER BY id DESC LIMIT ?"
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return Page{}, err
	}
	defer rows.Close()

	events := make([]Event, 0, limit)
	for rows.Next() {
		event, err := scanEvent(rows)
		if err != nil {
			return Page{}, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return Page{}, err
	}
	page := Page{Events: events}
	if len(events) > limit {
		next := events[limit-1].ID
		page.NextBeforeID = &next
		page.Events = events[:limit]
	}
	return page, nil
}

func (s *Store) DeleteOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM audit_events WHERE created_at < ?`, formatTime(cutoff.UTC()))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanEvent(row rowScanner) (Event, error) {
	var event Event
	var createdAt string
	var metadata string
	if err := row.Scan(&event.ID, &event.RequestID, &createdAt, &event.ActorType,
		&event.ActorName, &event.SourceIP, &event.Module, &event.Action,
		&event.TargetType, &event.TargetID, &event.TargetName, &event.NodeID,
		&event.Result, &event.DurationMS, &event.Summary, &metadata); err != nil {
		return Event{}, err
	}
	parsed, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return Event{}, fmt.Errorf("parse audit timestamp: %w", err)
	}
	event.CreatedAt = parsed.UTC()
	event.Metadata = make(map[string]string)
	if metadata != "" {
		if err := json.Unmarshal([]byte(metadata), &event.Metadata); err != nil {
			return Event{}, fmt.Errorf("decode audit metadata: %w", err)
		}
	}
	if event.Metadata == nil {
		event.Metadata = make(map[string]string)
	}
	return event, nil
}

func escapeLike(value string) string {
	value = strings.ReplaceAll(value, "!", "!!")
	value = strings.ReplaceAll(value, "%", "!%")
	return strings.ReplaceAll(value, "_", "!_")
}

func formatTime(value time.Time) string {
	return value.UTC().Format(databaseTimeFormat)
}
