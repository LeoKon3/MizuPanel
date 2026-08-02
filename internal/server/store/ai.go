package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	serverdb "github.com/mizupanel/mizupanel/internal/server/db"
)

var (
	ErrAIInvalid  = errors.New("invalid AI resource")
	ErrAINotFound = errors.New("AI resource not found")
	ErrAIConflict = errors.New("AI resource conflict")
)

type AIProvider struct {
	ID               string     `json:"id"`
	Name             string     `json:"name"`
	Protocol         string     `json:"protocol"`
	BaseURL          string     `json:"base_url"`
	Model            string     `json:"model"`
	APIKeyCiphertext string     `json:"-"`
	HasAPIKey        bool       `json:"has_api_key"`
	Default          bool       `json:"is_default"`
	ChatCapable      bool       `json:"chat_capable"`
	ToolsCapable     bool       `json:"tools_capable"`
	ProbeStatus      string     `json:"probe_status"`
	ProbedAt         *time.Time `json:"probed_at"`
	ProbeError       string     `json:"probe_error"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type AIConversation struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type AITurn struct {
	ID             string    `json:"id"`
	ConversationID string    `json:"conversation_id"`
	ProviderID     string    `json:"provider_id"`
	ProviderName   string    `json:"provider_name"`
	Protocol       string    `json:"protocol"`
	Model          string    `json:"model"`
	Status         string    `json:"status"`
	ErrorCode      string    `json:"error_code"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type AIMessage struct {
	ID             string    `json:"id"`
	ConversationID string    `json:"conversation_id"`
	TurnID         string    `json:"turn_id"`
	Role           string    `json:"role"`
	Content        string    `json:"content"`
	ProviderName   string    `json:"provider_name"`
	Model          string    `json:"model"`
	CreatedAt      time.Time `json:"created_at"`
}

type AIToolCall struct {
	ID             string    `json:"id"`
	TurnID         string    `json:"turn_id"`
	ProviderCallID string    `json:"-"`
	ToolName       string    `json:"tool_name"`
	Risk           string    `json:"risk"`
	Status         string    `json:"status"`
	ArgumentsJSON  string    `json:"-"`
	TargetType     string    `json:"target_type"`
	TargetID       string    `json:"target_id"`
	TargetName     string    `json:"target_name"`
	NodeID         string    `json:"node_id"`
	ResultSummary  string    `json:"result_summary"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type AIStore struct {
	db      *sql.DB
	dialect serverdb.Dialect
}

func NewAIStore(db *sql.DB, dialect serverdb.Dialect) *AIStore {
	return &AIStore{db: db, dialect: dialect}
}

func (s *AIStore) ProviderSecretCount(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_providers WHERE api_key_ciphertext <> ''`).Scan(&count)
	return count, err
}

func (s *AIStore) CreateProvider(ctx context.Context, provider AIProvider) (AIProvider, error) {
	if provider.ID == "" {
		provider.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	provider.CreatedAt = now
	provider.UpdatedAt = now
	query := `INSERT INTO ai_providers
		(id, name, normalized_name, protocol, base_url, model, api_key_ciphertext,
		 is_default, chat_capable, tools_capable, probe_status, probed_at, probe_error,
		 created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	arguments := []any{provider.ID, provider.Name, normalizedAIName(provider.Name), provider.Protocol,
		provider.BaseURL, provider.Model, provider.APIKeyCiphertext, provider.Default,
		provider.ChatCapable, provider.ToolsCapable, defaultProbeStatus(provider.ProbeStatus),
		formatOptionalTime(provider.ProbedAt), provider.ProbeError, formatTime(now), formatTime(now)}
	if s.dialect == serverdb.DialectMySQL {
		query = `INSERT INTO ai_providers
			(id, name, normalized_name, protocol, base_url, model, api_key_ciphertext,
			 is_default, default_marker, chat_capable, tools_capable, probe_status, probed_at,
			 probe_error, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
		arguments = []any{provider.ID, provider.Name, normalizedAIName(provider.Name), provider.Protocol,
			provider.BaseURL, provider.Model, provider.APIKeyCiphertext, provider.Default, aiDefaultMarker(provider.Default),
			provider.ChatCapable, provider.ToolsCapable, defaultProbeStatus(provider.ProbeStatus),
			formatOptionalTime(provider.ProbedAt), provider.ProbeError, formatTime(now), formatTime(now)}
	}
	_, err := s.db.ExecContext(ctx, query, arguments...)
	if err != nil {
		if isUniqueError(err) {
			return AIProvider{}, ErrAIConflict
		}
		return AIProvider{}, err
	}
	provider.HasAPIKey = provider.APIKeyCiphertext != ""
	return provider, nil
}

func (s *AIStore) UpdateProvider(ctx context.Context, provider AIProvider) (AIProvider, error) {
	now := time.Now().UTC()
	query := `UPDATE ai_providers SET name = ?, normalized_name = ?,
		protocol = ?, base_url = ?, model = ?, api_key_ciphertext = ?, is_default = ?,
		chat_capable = ?, tools_capable = ?, probe_status = ?, probed_at = ?, probe_error = ?,
		updated_at = ? WHERE id = ?`
	arguments := []any{provider.Name, normalizedAIName(provider.Name), provider.Protocol, provider.BaseURL,
		provider.Model, provider.APIKeyCiphertext, provider.Default, provider.ChatCapable,
		provider.ToolsCapable, defaultProbeStatus(provider.ProbeStatus),
		formatOptionalTime(provider.ProbedAt), provider.ProbeError, formatTime(now), provider.ID}
	if s.dialect == serverdb.DialectMySQL {
		query = `UPDATE ai_providers SET name = ?, normalized_name = ?, protocol = ?, base_url = ?,
			model = ?, api_key_ciphertext = ?, is_default = ?, default_marker = ?, chat_capable = ?,
			tools_capable = ?, probe_status = ?, probed_at = ?, probe_error = ?, updated_at = ? WHERE id = ?`
		arguments = []any{provider.Name, normalizedAIName(provider.Name), provider.Protocol, provider.BaseURL,
			provider.Model, provider.APIKeyCiphertext, provider.Default, aiDefaultMarker(provider.Default),
			provider.ChatCapable, provider.ToolsCapable, defaultProbeStatus(provider.ProbeStatus),
			formatOptionalTime(provider.ProbedAt), provider.ProbeError, formatTime(now), provider.ID}
	}
	result, err := s.db.ExecContext(ctx, query, arguments...)
	if err != nil {
		if isUniqueError(err) {
			return AIProvider{}, ErrAIConflict
		}
		return AIProvider{}, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return AIProvider{}, err
	}
	if rows == 0 {
		return AIProvider{}, ErrAINotFound
	}
	return s.GetProvider(ctx, provider.ID)
}

func (s *AIStore) GetProvider(ctx context.Context, id string) (AIProvider, error) {
	return scanAIProvider(s.db.QueryRowContext(ctx, aiProviderSelect+` WHERE id = ?`, id))
}

func (s *AIStore) DefaultProvider(ctx context.Context) (AIProvider, error) {
	return scanAIProvider(s.db.QueryRowContext(ctx, aiProviderSelect+` WHERE is_default = 1 LIMIT 1`))
}

func (s *AIStore) ListProviders(ctx context.Context) ([]AIProvider, error) {
	rows, err := s.db.QueryContext(ctx, aiProviderSelect+` ORDER BY is_default DESC, name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	providers := make([]AIProvider, 0)
	for rows.Next() {
		provider, err := scanAIProvider(rows)
		if err != nil {
			return nil, err
		}
		providers = append(providers, provider)
	}
	return providers, rows.Err()
}

func (s *AIStore) DeleteProvider(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM ai_providers WHERE id = ?`, id)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrAINotFound
	}
	return nil
}

func (s *AIStore) SetDefaultProvider(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var chat, tools bool
	if err := tx.QueryRowContext(ctx, `SELECT chat_capable, tools_capable FROM ai_providers WHERE id = ?`, id).Scan(&chat, &tools); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrAINotFound
		}
		return err
	}
	if !chat || !tools {
		return ErrAIInvalid
	}
	clearQuery := `UPDATE ai_providers SET is_default = 0 WHERE is_default = 1`
	setQuery := `UPDATE ai_providers SET is_default = 1, updated_at = ? WHERE id = ?`
	if s.dialect == serverdb.DialectMySQL {
		clearQuery = `UPDATE ai_providers SET is_default = 0, default_marker = NULL WHERE is_default = 1`
		setQuery = `UPDATE ai_providers SET is_default = 1, default_marker = '1', updated_at = ? WHERE id = ?`
	}
	if _, err := tx.ExecContext(ctx, clearQuery); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, setQuery, formatTime(time.Now().UTC()), id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *AIStore) SaveProviderProbe(ctx context.Context, id string, chat, tools bool, status, safeError string, now time.Time) error {
	result, err := s.db.ExecContext(ctx, `UPDATE ai_providers SET chat_capable = ?, tools_capable = ?,
		probe_status = ?, probed_at = ?, probe_error = ?, updated_at = ? WHERE id = ?`,
		chat, tools, status, formatTime(now.UTC()), safeError, formatTime(now.UTC()), id)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrAINotFound
	}
	return nil
}

func (s *AIStore) CreateConversation(ctx context.Context, title string) (AIConversation, error) {
	now := time.Now().UTC()
	conversation := AIConversation{ID: uuid.NewString(), Title: title, CreatedAt: now, UpdatedAt: now}
	_, err := s.db.ExecContext(ctx, `INSERT INTO ai_conversations (id, title, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		conversation.ID, conversation.Title, formatTime(now), formatTime(now))
	return conversation, err
}

func (s *AIStore) GetConversation(ctx context.Context, id string) (AIConversation, error) {
	var conversation AIConversation
	var created, updated string
	err := s.db.QueryRowContext(ctx, `SELECT id, title, created_at, updated_at FROM ai_conversations WHERE id = ?`, id).
		Scan(&conversation.ID, &conversation.Title, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return AIConversation{}, ErrAINotFound
	}
	if err != nil {
		return AIConversation{}, err
	}
	if conversation.CreatedAt, err = parseTime(created); err != nil {
		return AIConversation{}, err
	}
	if conversation.UpdatedAt, err = parseTime(updated); err != nil {
		return AIConversation{}, err
	}
	return conversation, nil
}

func (s *AIStore) ListConversations(ctx context.Context, limit int) ([]AIConversation, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, title, created_at, updated_at FROM ai_conversations ORDER BY updated_at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]AIConversation, 0)
	for rows.Next() {
		var item AIConversation
		var created, updated string
		if err := rows.Scan(&item.ID, &item.Title, &created, &updated); err != nil {
			return nil, err
		}
		if item.CreatedAt, err = parseTime(created); err != nil {
			return nil, err
		}
		if item.UpdatedAt, err = parseTime(updated); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *AIStore) RenameConversation(ctx context.Context, id, title string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE ai_conversations SET title = ?, updated_at = ? WHERE id = ?`, title, formatTime(time.Now().UTC()), id)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrAINotFound
	}
	return nil
}

func (s *AIStore) DeleteConversation(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM ai_conversations WHERE id = ?`, id)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrAINotFound
	}
	return nil
}

func (s *AIStore) StartTurn(ctx context.Context, conversationID string, provider AIProvider, userContent string) (AITurn, AIMessage, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AITurn{}, AIMessage{}, err
	}
	defer tx.Rollback()
	var existing string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM ai_conversations WHERE id = ?`, conversationID).Scan(&existing); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return AITurn{}, AIMessage{}, ErrAINotFound
		}
		return AITurn{}, AIMessage{}, err
	}
	now := time.Now().UTC()
	turn := AITurn{ID: uuid.NewString(), ConversationID: conversationID, ProviderID: provider.ID,
		ProviderName: provider.Name, Protocol: provider.Protocol, Model: provider.Model,
		Status: "running", CreatedAt: now, UpdatedAt: now}
	if s.dialect == serverdb.DialectMySQL {
		_, err = tx.ExecContext(ctx, `INSERT INTO ai_turns
			(id, conversation_id, provider_id, provider_name, protocol, model, status, error_code, active_marker, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, 'running', '', ?, ?, ?)`, turn.ID, conversationID, provider.ID,
			provider.Name, provider.Protocol, provider.Model, "1", formatTime(now), formatTime(now))
	} else {
		_, err = tx.ExecContext(ctx, `INSERT INTO ai_turns
			(id, conversation_id, provider_id, provider_name, protocol, model, status, error_code, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, 'running', '', ?, ?)`, turn.ID, conversationID, provider.ID,
			provider.Name, provider.Protocol, provider.Model, formatTime(now), formatTime(now))
	}
	if err != nil {
		if isUniqueError(err) {
			return AITurn{}, AIMessage{}, ErrAIConflict
		}
		return AITurn{}, AIMessage{}, err
	}
	message := AIMessage{ID: uuid.NewString(), ConversationID: conversationID, TurnID: turn.ID,
		Role: "user", Content: userContent, ProviderName: provider.Name, Model: provider.Model, CreatedAt: now}
	if _, err := tx.ExecContext(ctx, `INSERT INTO ai_messages
		(id, conversation_id, turn_id, role, content, provider_name, model, created_at)
		VALUES (?, ?, ?, 'user', ?, ?, ?, ?)`, message.ID, conversationID, turn.ID, userContent,
		provider.Name, provider.Model, formatTime(now)); err != nil {
		return AITurn{}, AIMessage{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE ai_conversations SET updated_at = ? WHERE id = ?`, formatTime(now), conversationID); err != nil {
		return AITurn{}, AIMessage{}, err
	}
	return turn, message, tx.Commit()
}

func (s *AIStore) CompleteTurn(ctx context.Context, turn AITurn, content string) (AIMessage, error) {
	return s.finishTurn(ctx, turn, "completed", "", content)
}

func (s *AIStore) FailTurn(ctx context.Context, turn AITurn, errorCode string) error {
	_, err := s.finishTurn(ctx, turn, "failed", errorCode, "")
	return err
}

func (s *AIStore) finishTurn(ctx context.Context, turn AITurn, status, errorCode, content string) (AIMessage, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AIMessage{}, err
	}
	defer tx.Rollback()
	now := time.Now().UTC()
	query := `UPDATE ai_turns SET status = ?, error_code = ?, updated_at = ? WHERE id = ? AND status IN ('running', 'awaiting_confirmation')`
	if s.dialect == serverdb.DialectMySQL {
		query = `UPDATE ai_turns SET status = ?, error_code = ?, active_marker = NULL, updated_at = ? WHERE id = ? AND status IN ('running', 'awaiting_confirmation')`
	}
	result, err := tx.ExecContext(ctx, query, status, errorCode, formatTime(now), turn.ID)
	if err != nil {
		return AIMessage{}, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return AIMessage{}, err
	}
	if rows == 0 {
		return AIMessage{}, ErrAIConflict
	}
	message := AIMessage{}
	if content != "" {
		message = AIMessage{ID: uuid.NewString(), ConversationID: turn.ConversationID, TurnID: turn.ID,
			Role: "assistant", Content: content, ProviderName: turn.ProviderName, Model: turn.Model, CreatedAt: now}
		if _, err := tx.ExecContext(ctx, `INSERT INTO ai_messages
			(id, conversation_id, turn_id, role, content, provider_name, model, created_at)
			VALUES (?, ?, ?, 'assistant', ?, ?, ?, ?)`, message.ID, turn.ConversationID, turn.ID,
			content, turn.ProviderName, turn.Model, formatTime(now)); err != nil {
			return AIMessage{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE ai_conversations SET updated_at = ? WHERE id = ?`, formatTime(now), turn.ConversationID); err != nil {
		return AIMessage{}, err
	}
	return message, tx.Commit()
}

func (s *AIStore) ListMessages(ctx context.Context, conversationID string, limit int) ([]AIMessage, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, conversation_id, turn_id, role, content, provider_name, model, created_at
		FROM (SELECT id, conversation_id, turn_id, role, content, provider_name, model, created_at
			FROM ai_messages WHERE conversation_id = ? ORDER BY created_at DESC, id DESC LIMIT ?) recent
		ORDER BY created_at ASC, id ASC`, conversationID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]AIMessage, 0)
	for rows.Next() {
		message, err := scanAIMessage(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, message)
	}
	return result, rows.Err()
}

func (s *AIStore) CreateToolCall(ctx context.Context, turn AITurn, call AIToolCall) (AIToolCall, error) {
	if call.ID == "" {
		call.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	call.TurnID = turn.ID
	call.CreatedAt = now
	call.UpdatedAt = now
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AIToolCall{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO ai_tool_calls
		(id, turn_id, provider_call_id, tool_name, risk, status, arguments_json, target_type,
		 target_id, target_name, node_id, result_summary, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, call.ID, call.TurnID,
		call.ProviderCallID, call.ToolName, call.Risk, call.Status, call.ArgumentsJSON,
		call.TargetType, call.TargetID, call.TargetName, call.NodeID, call.ResultSummary,
		formatTime(now), formatTime(now)); err != nil {
		return AIToolCall{}, err
	}
	if call.Status == "pending" {
		query := `UPDATE ai_turns SET status = 'awaiting_confirmation', updated_at = ? WHERE id = ? AND status = 'running'`
		if s.dialect == serverdb.DialectMySQL {
			query = `UPDATE ai_turns SET status = 'awaiting_confirmation', active_marker = ?, updated_at = ? WHERE id = ? AND status = 'running'`
			_, err = tx.ExecContext(ctx, query, "1", formatTime(now), turn.ID)
		} else {
			_, err = tx.ExecContext(ctx, query, formatTime(now), turn.ID)
		}
		if err != nil {
			return AIToolCall{}, err
		}
	}
	return call, tx.Commit()
}

func (s *AIStore) UpdateToolCallResult(ctx context.Context, id, status, summary string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE ai_tool_calls SET status = ?, result_summary = ?, updated_at = ? WHERE id = ?`,
		status, summary, formatTime(time.Now().UTC()), id)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrAINotFound
	}
	return nil
}

func (s *AIStore) GetToolCall(ctx context.Context, id string) (AIToolCall, AITurn, error) {
	row := s.db.QueryRowContext(ctx, `SELECT c.id, c.turn_id, c.provider_call_id, c.tool_name, c.risk,
		c.status, c.arguments_json, c.target_type, c.target_id, c.target_name, c.node_id,
		c.result_summary, c.created_at, c.updated_at, t.conversation_id, t.provider_id,
		t.provider_name, t.protocol, t.model, t.status, t.error_code, t.created_at, t.updated_at
		FROM ai_tool_calls c JOIN ai_turns t ON t.id = c.turn_id WHERE c.id = ?`, id)
	var call AIToolCall
	var turn AITurn
	var callCreated, callUpdated, turnCreated, turnUpdated string
	err := row.Scan(&call.ID, &call.TurnID, &call.ProviderCallID, &call.ToolName, &call.Risk,
		&call.Status, &call.ArgumentsJSON, &call.TargetType, &call.TargetID, &call.TargetName,
		&call.NodeID, &call.ResultSummary, &callCreated, &callUpdated, &turn.ConversationID,
		&turn.ProviderID, &turn.ProviderName, &turn.Protocol, &turn.Model, &turn.Status,
		&turn.ErrorCode, &turnCreated, &turnUpdated)
	if errors.Is(err, sql.ErrNoRows) {
		return AIToolCall{}, AITurn{}, ErrAINotFound
	}
	if err != nil {
		return AIToolCall{}, AITurn{}, err
	}
	turn.ID = call.TurnID
	if call.CreatedAt, err = parseTime(callCreated); err != nil {
		return AIToolCall{}, AITurn{}, err
	}
	if call.UpdatedAt, err = parseTime(callUpdated); err != nil {
		return AIToolCall{}, AITurn{}, err
	}
	if turn.CreatedAt, err = parseTime(turnCreated); err != nil {
		return AIToolCall{}, AITurn{}, err
	}
	if turn.UpdatedAt, err = parseTime(turnUpdated); err != nil {
		return AIToolCall{}, AITurn{}, err
	}
	return call, turn, nil
}

func (s *AIStore) ListToolCalls(ctx context.Context, conversationID string) ([]AIToolCall, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT c.id, c.turn_id, c.provider_call_id, c.tool_name,
		c.risk, c.status, c.arguments_json, c.target_type, c.target_id, c.target_name, c.node_id,
		c.result_summary, c.created_at, c.updated_at FROM ai_tool_calls c
		JOIN ai_turns t ON t.id = c.turn_id WHERE t.conversation_id = ? ORDER BY c.created_at, c.id`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]AIToolCall, 0)
	for rows.Next() {
		call, err := scanAIToolCall(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, call)
	}
	return result, rows.Err()
}

func (s *AIStore) ClaimToolCall(ctx context.Context, id string) (AIToolCall, AITurn, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE ai_tool_calls SET status = 'running', updated_at = ? WHERE id = ? AND status = 'pending'`,
		formatTime(time.Now().UTC()), id)
	if err != nil {
		return AIToolCall{}, AITurn{}, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return AIToolCall{}, AITurn{}, err
	}
	if rows == 0 {
		return AIToolCall{}, AITurn{}, ErrAIConflict
	}
	return s.GetToolCall(ctx, id)
}

func (s *AIStore) RejectToolCall(ctx context.Context, id string) (AIToolCall, AITurn, error) {
	call, turn, err := s.ClaimToolCall(ctx, id)
	if err != nil {
		return AIToolCall{}, AITurn{}, err
	}
	if err := s.UpdateToolCallResult(ctx, id, "rejected", "操作已取消"); err != nil {
		return AIToolCall{}, AITurn{}, err
	}
	call.Status = "rejected"
	call.ResultSummary = "操作已取消"
	return call, turn, nil
}

func (s *AIStore) RecoverInterrupted(ctx context.Context) error {
	now := formatTime(time.Now().UTC())
	if s.dialect == serverdb.DialectMySQL {
		if _, err := s.db.ExecContext(ctx, `UPDATE ai_turns SET status = 'interrupted', error_code = 'server_restarted', active_marker = NULL, updated_at = ? WHERE status IN ('running', 'awaiting_confirmation')`, now); err != nil {
			return err
		}
	} else if _, err := s.db.ExecContext(ctx, `UPDATE ai_turns SET status = 'interrupted', error_code = 'server_restarted', updated_at = ? WHERE status IN ('running', 'awaiting_confirmation')`, now); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE ai_tool_calls SET status = 'interrupted', result_summary = '服务重启，操作未执行', updated_at = ? WHERE status IN ('running', 'pending')`, now)
	return err
}

const aiProviderSelect = `SELECT id, name, protocol, base_url, model, api_key_ciphertext,
	is_default, chat_capable, tools_capable, probe_status, probed_at, probe_error, created_at, updated_at
	FROM ai_providers`

type aiScanner interface {
	Scan(dest ...any) error
}

func scanAIProvider(scanner aiScanner) (AIProvider, error) {
	var provider AIProvider
	var probed sql.NullString
	var created, updated string
	err := scanner.Scan(&provider.ID, &provider.Name, &provider.Protocol, &provider.BaseURL,
		&provider.Model, &provider.APIKeyCiphertext, &provider.Default, &provider.ChatCapable,
		&provider.ToolsCapable, &provider.ProbeStatus, &probed, &provider.ProbeError,
		&created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return AIProvider{}, ErrAINotFound
	}
	if err != nil {
		return AIProvider{}, err
	}
	provider.HasAPIKey = provider.APIKeyCiphertext != ""
	if probed.Valid && probed.String != "" {
		value, err := parseTime(probed.String)
		if err != nil {
			return AIProvider{}, err
		}
		provider.ProbedAt = &value
	}
	if provider.CreatedAt, err = parseTime(created); err != nil {
		return AIProvider{}, err
	}
	if provider.UpdatedAt, err = parseTime(updated); err != nil {
		return AIProvider{}, err
	}
	return provider, nil
}

func scanAIMessage(scanner aiScanner) (AIMessage, error) {
	var message AIMessage
	var created string
	if err := scanner.Scan(&message.ID, &message.ConversationID, &message.TurnID, &message.Role,
		&message.Content, &message.ProviderName, &message.Model, &created); err != nil {
		return AIMessage{}, err
	}
	parsed, err := parseTime(created)
	if err != nil {
		return AIMessage{}, err
	}
	message.CreatedAt = parsed
	return message, nil
}

func scanAIToolCall(scanner aiScanner) (AIToolCall, error) {
	var call AIToolCall
	var created, updated string
	if err := scanner.Scan(&call.ID, &call.TurnID, &call.ProviderCallID, &call.ToolName,
		&call.Risk, &call.Status, &call.ArgumentsJSON, &call.TargetType, &call.TargetID,
		&call.TargetName, &call.NodeID, &call.ResultSummary, &created, &updated); err != nil {
		return AIToolCall{}, err
	}
	var err error
	if call.CreatedAt, err = parseTime(created); err != nil {
		return AIToolCall{}, err
	}
	if call.UpdatedAt, err = parseTime(updated); err != nil {
		return AIToolCall{}, err
	}
	return call, nil
}

func normalizedAIName(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func defaultProbeStatus(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}

func formatOptionalTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return formatTime(value.UTC())
}

func aiDefaultMarker(value bool) any {
	if value {
		return "1"
	}
	return nil
}

func isUniqueError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique constraint") || strings.Contains(message, "duplicate entry")
}

func ValidateAITitle(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 255 {
		return "", fmt.Errorf("%w: title", ErrAIInvalid)
	}
	return value, nil
}
