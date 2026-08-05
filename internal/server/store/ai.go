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
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	Protocol         string            `json:"protocol"`
	BaseURL          string            `json:"base_url"`
	Enabled          bool              `json:"enabled"`
	DiscoveryStatus  string            `json:"discovery_status"`
	DiscoveryLatency int               `json:"discovery_latency_ms"`
	DiscoveredAt     *time.Time        `json:"discovered_at"`
	DiscoveryError   string            `json:"discovery_error"`
	Models           []AIProviderModel `json:"models"`
	// The fields below mirror one child model for one-release compatibility.
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

type AIProviderModel struct {
	ID           string     `json:"id"`
	ProviderID   string     `json:"provider_id"`
	ModelID      string     `json:"model_id"`
	DisplayName  string     `json:"display_name"`
	Enabled      bool       `json:"enabled"`
	ChatCapable  bool       `json:"chat_capable"`
	ToolsCapable bool       `json:"tools_capable"`
	ProbeStatus  string     `json:"probe_status"`
	ProbeLatency int        `json:"probe_latency_ms"`
	ProbedAt     *time.Time `json:"probed_at"`
	ProbeError   string     `json:"probe_error"`
	Default      bool       `json:"is_default"`
	Fallback     bool       `json:"is_fallback"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

type AIRouting struct {
	DefaultModelID  *string `json:"default_model_id"`
	FallbackModelID *string `json:"fallback_model_id"`
}

type AIConversation struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	ModelID   *string   `json:"model_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type AITurn struct {
	ID                    string    `json:"id"`
	ConversationID        string    `json:"conversation_id"`
	ModelID               *string   `json:"model_id"`
	ProviderID            string    `json:"provider_id"`
	ProviderName          string    `json:"provider_name"`
	Protocol              string    `json:"protocol"`
	Model                 string    `json:"model"`
	RequestedProviderID   string    `json:"requested_provider_id"`
	RequestedProviderName string    `json:"requested_provider_name"`
	RequestedModelID      *string   `json:"requested_model_id"`
	RequestedModel        string    `json:"requested_model"`
	FallbackUsed          bool      `json:"fallback_used"`
	Status                string    `json:"status"`
	ErrorCode             string    `json:"error_code"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
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
	provider.DiscoveryStatus = defaultProbeStatus(provider.DiscoveryStatus)
	provider.CreatedAt = now
	provider.UpdatedAt = now
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AIProvider{}, err
	}
	defer tx.Rollback()
	query := `INSERT INTO ai_providers
		(id, name, normalized_name, protocol, base_url, model, api_key_ciphertext,
		 enabled, discovery_status, discovery_latency_ms, discovered_at, discovery_error,
		 is_default, chat_capable, tools_capable, probe_status, probed_at, probe_error,
		 created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	arguments := []any{provider.ID, provider.Name, normalizedAIName(provider.Name), provider.Protocol,
		provider.BaseURL, provider.Model, provider.APIKeyCiphertext, provider.Enabled,
		provider.DiscoveryStatus, boundedLatency(provider.DiscoveryLatency), formatOptionalTime(provider.DiscoveredAt), provider.DiscoveryError, provider.Default,
		provider.ChatCapable, provider.ToolsCapable, defaultProbeStatus(provider.ProbeStatus),
		formatOptionalTime(provider.ProbedAt), provider.ProbeError, formatTime(now), formatTime(now)}
	if s.dialect == serverdb.DialectMySQL {
		query = `INSERT INTO ai_providers
			(id, name, normalized_name, protocol, base_url, model, api_key_ciphertext,
			 enabled, discovery_status, discovery_latency_ms, discovered_at, discovery_error,
			 is_default, default_marker, chat_capable, tools_capable, probe_status, probed_at,
			 probe_error, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
		arguments = []any{provider.ID, provider.Name, normalizedAIName(provider.Name), provider.Protocol,
			provider.BaseURL, provider.Model, provider.APIKeyCiphertext, provider.Enabled,
			provider.DiscoveryStatus, boundedLatency(provider.DiscoveryLatency), formatOptionalTime(provider.DiscoveredAt), provider.DiscoveryError,
			provider.Default, aiDefaultMarker(provider.Default),
			provider.ChatCapable, provider.ToolsCapable, defaultProbeStatus(provider.ProbeStatus),
			formatOptionalTime(provider.ProbedAt), provider.ProbeError, formatTime(now), formatTime(now)}
	}
	_, err = tx.ExecContext(ctx, query, arguments...)
	if err != nil {
		if isUniqueError(err) {
			return AIProvider{}, ErrAIConflict
		}
		return AIProvider{}, err
	}
	if strings.TrimSpace(provider.Model) != "" {
		model := AIProviderModel{ID: provider.ID, ProviderID: provider.ID, ModelID: provider.Model,
			Enabled: true, ChatCapable: provider.ChatCapable, ToolsCapable: provider.ToolsCapable,
			ProbeStatus: defaultProbeStatus(provider.ProbeStatus), ProbedAt: provider.ProbedAt,
			ProbeError: provider.ProbeError, Default: provider.Default, CreatedAt: now, UpdatedAt: now}
		if err := s.insertModelTx(ctx, tx, model); err != nil {
			if isUniqueError(err) {
				return AIProvider{}, ErrAIConflict
			}
			return AIProvider{}, err
		}
		provider.Models = []AIProviderModel{model}
	}
	if err := tx.Commit(); err != nil {
		return AIProvider{}, err
	}
	provider.HasAPIKey = provider.APIKeyCiphertext != ""
	return provider, nil
}

func (s *AIStore) UpdateProvider(ctx context.Context, provider AIProvider) (AIProvider, error) {
	return s.UpdateProviderConnection(ctx, provider, false)
}

func (s *AIStore) UpdateProviderConnection(ctx context.Context, provider AIProvider, invalidateModels bool) (AIProvider, error) {
	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AIProvider{}, err
	}
	defer tx.Rollback()
	query := `UPDATE ai_providers SET name = ?, normalized_name = ?, protocol = ?, base_url = ?,
		api_key_ciphertext = ?, enabled = ?, discovery_status = ?, discovery_latency_ms = ?,
		discovered_at = ?, discovery_error = ?, updated_at = ? WHERE id = ?`
	arguments := []any{provider.Name, normalizedAIName(provider.Name), provider.Protocol, provider.BaseURL,
		provider.APIKeyCiphertext, provider.Enabled, defaultProbeStatus(provider.DiscoveryStatus),
		boundedLatency(provider.DiscoveryLatency), formatOptionalTime(provider.DiscoveredAt), provider.DiscoveryError,
		formatTime(now), provider.ID}
	result, err := tx.ExecContext(ctx, query, arguments...)
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
	if invalidateModels {
		if _, err := tx.ExecContext(ctx, `UPDATE ai_provider_models SET chat_capable = 0, tools_capable = 0,
			probe_status = 'unknown', probe_latency_ms = 0, probed_at = NULL, probe_error = '',
			is_default = 0, is_fallback = 0, updated_at = ? WHERE provider_id = ?`, formatTime(now), provider.ID); err != nil {
			return AIProvider{}, err
		}
		if s.dialect == serverdb.DialectMySQL {
			if _, err := tx.ExecContext(ctx, `UPDATE ai_provider_models SET default_marker = NULL, fallback_marker = NULL WHERE provider_id = ?`, provider.ID); err != nil {
				return AIProvider{}, err
			}
		}
	} else if !provider.Enabled {
		query := `UPDATE ai_provider_models SET is_default = 0, is_fallback = 0, updated_at = ? WHERE provider_id = ?`
		if s.dialect == serverdb.DialectMySQL {
			query = `UPDATE ai_provider_models SET is_default = 0, is_fallback = 0,
				default_marker = NULL, fallback_marker = NULL, updated_at = ? WHERE provider_id = ?`
		}
		if _, err := tx.ExecContext(ctx, query, formatTime(now), provider.ID); err != nil {
			return AIProvider{}, err
		}
	}
	if err := s.syncProviderCompatibilityTx(ctx, tx, provider.ID); err != nil {
		return AIProvider{}, err
	}
	if err := tx.Commit(); err != nil {
		return AIProvider{}, err
	}
	return s.GetProvider(ctx, provider.ID)
}

func (s *AIStore) GetProvider(ctx context.Context, id string) (AIProvider, error) {
	provider, err := scanAIProvider(s.db.QueryRowContext(ctx, aiProviderSelect+` WHERE id = ?`, id))
	if err != nil {
		return AIProvider{}, err
	}
	provider.Models, err = s.ListProviderModels(ctx, id)
	return provider, err
}

func (s *AIStore) DefaultProvider(ctx context.Context) (AIProvider, error) {
	return scanAIProvider(s.db.QueryRowContext(ctx, aiProviderSelect+` WHERE is_default = 1 LIMIT 1`))
}

func (s *AIStore) ListProviders(ctx context.Context) ([]AIProvider, error) {
	rows, err := s.db.QueryContext(ctx, aiProviderSelect+` ORDER BY is_default DESC, name ASC`)
	if err != nil {
		return nil, err
	}
	providers := make([]AIProvider, 0)
	for rows.Next() {
		provider, err := scanAIProvider(rows)
		if err != nil {
			return nil, err
		}
		providers = append(providers, provider)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for index := range providers {
		providers[index].Models, err = s.ListProviderModels(ctx, providers[index].ID)
		if err != nil {
			return nil, err
		}
	}
	return providers, nil
}

func (s *AIStore) DeleteProvider(ctx context.Context, id string) error {
	var special int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_provider_models WHERE provider_id = ? AND (is_default = 1 OR is_fallback = 1)`, id).Scan(&special); err != nil {
		return err
	}
	if special > 0 {
		return ErrAIConflict
	}
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
	model, err := s.compatibilityModel(ctx, id)
	if err != nil {
		return err
	}
	routing, err := s.GetRouting(ctx)
	if err != nil {
		return err
	}
	return s.SetRouting(ctx, stringPointer(model.ID), routing.FallbackModelID)
}

func (s *AIStore) SaveProviderProbe(ctx context.Context, id string, chat, tools bool, status, safeError string, now time.Time) error {
	model, err := s.compatibilityModel(ctx, id)
	if err != nil {
		return err
	}
	return s.SaveModelProbe(ctx, model.ID, chat, tools, status, 0, safeError, now)
}

func (s *AIStore) SaveProviderDiscovery(ctx context.Context, id, status string, latency int, safeError string, now time.Time) error {
	result, err := s.db.ExecContext(ctx, `UPDATE ai_providers SET discovery_status = ?, discovery_latency_ms = ?,
		discovered_at = ?, discovery_error = ?, updated_at = ? WHERE id = ?`, defaultProbeStatus(status),
		boundedLatency(latency), formatTime(now.UTC()), safeError, formatTime(now.UTC()), id)
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

func (s *AIStore) CreateProviderModels(ctx context.Context, providerID string, models []AIProviderModel) ([]AIProviderModel, error) {
	if len(models) == 0 || len(models) > 100 {
		return nil, ErrAIInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var exists string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM ai_providers WHERE id = ?`, providerID).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrAINotFound
		}
		return nil, err
	}
	now := time.Now().UTC()
	created := make([]AIProviderModel, 0, len(models))
	for _, model := range models {
		if model.ID == "" {
			model.ID = uuid.NewString()
		}
		model.ProviderID = providerID
		model.ProbeStatus = defaultProbeStatus(model.ProbeStatus)
		model.CreatedAt, model.UpdatedAt = now, now
		if err := s.insertModelTx(ctx, tx, model); err != nil {
			if isUniqueError(err) {
				return nil, ErrAIConflict
			}
			return nil, err
		}
		created = append(created, model)
	}
	if err := s.syncProviderCompatibilityTx(ctx, tx, providerID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return created, nil
}

func (s *AIStore) insertModelTx(ctx context.Context, tx *sql.Tx, model AIProviderModel) error {
	query := `INSERT INTO ai_provider_models
		(id, provider_id, model_id, display_name, enabled, chat_capable, tools_capable,
		 probe_status, probe_latency_ms, probed_at, probe_error, is_default, is_fallback,
		 created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	arguments := []any{model.ID, model.ProviderID, model.ModelID, model.DisplayName, model.Enabled,
		model.ChatCapable, model.ToolsCapable, defaultProbeStatus(model.ProbeStatus), boundedLatency(model.ProbeLatency),
		formatOptionalTime(model.ProbedAt), model.ProbeError, model.Default, model.Fallback,
		formatTime(model.CreatedAt), formatTime(model.UpdatedAt)}
	if s.dialect == serverdb.DialectMySQL {
		query = `INSERT INTO ai_provider_models
			(id, provider_id, model_id, display_name, enabled, chat_capable, tools_capable,
			 probe_status, probe_latency_ms, probed_at, probe_error, is_default, is_fallback,
			 default_marker, fallback_marker, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
		arguments = append(arguments[:13], aiDefaultMarker(model.Default), aiDefaultMarker(model.Fallback),
			formatTime(model.CreatedAt), formatTime(model.UpdatedAt))
	}
	_, err := tx.ExecContext(ctx, query, arguments...)
	return err
}

func (s *AIStore) GetModel(ctx context.Context, id string) (AIProviderModel, error) {
	return scanAIProviderModel(s.db.QueryRowContext(ctx, aiProviderModelSelect+` WHERE id = ?`, id))
}

func (s *AIStore) ListProviderModels(ctx context.Context, providerID string) ([]AIProviderModel, error) {
	rows, err := s.db.QueryContext(ctx, aiProviderModelSelect+` WHERE provider_id = ? ORDER BY is_default DESC, is_fallback DESC, created_at, id`, providerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	models := make([]AIProviderModel, 0)
	for rows.Next() {
		model, err := scanAIProviderModel(rows)
		if err != nil {
			return nil, err
		}
		models = append(models, model)
	}
	return models, rows.Err()
}

func (s *AIStore) UpdateModel(ctx context.Context, model AIProviderModel) (AIProviderModel, error) {
	existing, err := s.GetModel(ctx, model.ID)
	if err != nil {
		return AIProviderModel{}, err
	}
	if !model.Enabled && (existing.Default || existing.Fallback) {
		return AIProviderModel{}, ErrAIConflict
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AIProviderModel{}, err
	}
	defer tx.Rollback()
	now := time.Now().UTC()
	query := `UPDATE ai_provider_models SET model_id = ?, display_name = ?, enabled = ?, updated_at = ?`
	arguments := []any{model.ModelID, model.DisplayName, model.Enabled, formatTime(now)}
	if model.ModelID != existing.ModelID {
		query += `, chat_capable = 0, tools_capable = 0, probe_status = 'unknown', probe_latency_ms = 0,
			probed_at = NULL, probe_error = '', is_default = 0, is_fallback = 0`
		if s.dialect == serverdb.DialectMySQL {
			query += `, default_marker = NULL, fallback_marker = NULL`
		}
	}
	query += ` WHERE id = ?`
	arguments = append(arguments, model.ID)
	result, err := tx.ExecContext(ctx, query, arguments...)
	if err != nil {
		if isUniqueError(err) {
			return AIProviderModel{}, ErrAIConflict
		}
		return AIProviderModel{}, err
	}
	if rows, err := result.RowsAffected(); err != nil {
		return AIProviderModel{}, err
	} else if rows == 0 {
		return AIProviderModel{}, ErrAINotFound
	}
	if err := s.syncProviderCompatibilityTx(ctx, tx, existing.ProviderID); err != nil {
		return AIProviderModel{}, err
	}
	if err := tx.Commit(); err != nil {
		return AIProviderModel{}, err
	}
	return s.GetModel(ctx, model.ID)
}

func (s *AIStore) DeleteModel(ctx context.Context, id string) error {
	model, err := s.GetModel(ctx, id)
	if err != nil {
		return err
	}
	if model.Default || model.Fallback {
		return ErrAIConflict
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `DELETE FROM ai_provider_models WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if rows, err := result.RowsAffected(); err != nil {
		return err
	} else if rows == 0 {
		return ErrAINotFound
	}
	if err := s.syncProviderCompatibilityTx(ctx, tx, model.ProviderID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *AIStore) SaveModelProbe(ctx context.Context, id string, chat, tools bool, status string, latency int, safeError string, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var providerID string
	if err := tx.QueryRowContext(ctx, `SELECT provider_id FROM ai_provider_models WHERE id = ?`, id).Scan(&providerID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrAINotFound
		}
		return err
	}
	clearSpecial := !chat || !tools || status != "success"
	query := `UPDATE ai_provider_models SET chat_capable = ?, tools_capable = ?, probe_status = ?,
		probe_latency_ms = ?, probed_at = ?, probe_error = ?, updated_at = ?`
	arguments := []any{chat, tools, defaultProbeStatus(status), boundedLatency(latency), formatTime(now.UTC()), safeError, formatTime(now.UTC())}
	if clearSpecial {
		query += `, is_default = 0, is_fallback = 0`
		if s.dialect == serverdb.DialectMySQL {
			query += `, default_marker = NULL, fallback_marker = NULL`
		}
	}
	query += ` WHERE id = ?`
	arguments = append(arguments, id)
	if _, err := tx.ExecContext(ctx, query, arguments...); err != nil {
		return err
	}
	if err := s.syncProviderCompatibilityTx(ctx, tx, providerID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *AIStore) GetRouting(ctx context.Context) (AIRouting, error) {
	routing := AIRouting{}
	var defaultID, fallbackID sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT
		(SELECT id FROM ai_provider_models WHERE is_default = 1 LIMIT 1),
		(SELECT id FROM ai_provider_models WHERE is_fallback = 1 LIMIT 1)`).Scan(&defaultID, &fallbackID); err != nil {
		return AIRouting{}, err
	}
	routing.DefaultModelID = nullableStringPointer(defaultID)
	routing.FallbackModelID = nullableStringPointer(fallbackID)
	return routing, nil
}

func (s *AIStore) SetRouting(ctx context.Context, defaultID, fallbackID *string) error {
	if defaultID != nil && fallbackID != nil && *defaultID == *fallbackID {
		return ErrAIInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range []*string{defaultID, fallbackID} {
		if id == nil || *id == "" {
			continue
		}
		var providerEnabled, enabled, chat, tools bool
		if err := tx.QueryRowContext(ctx, `SELECT p.enabled, m.enabled, m.chat_capable, m.tools_capable
			FROM ai_provider_models m JOIN ai_providers p ON p.id = m.provider_id WHERE m.id = ?`, *id).
			Scan(&providerEnabled, &enabled, &chat, &tools); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrAINotFound
			}
			return err
		}
		if !providerEnabled || !enabled || !chat || !tools {
			return ErrAIInvalid
		}
	}
	clear := `UPDATE ai_provider_models SET is_default = 0, is_fallback = 0`
	if s.dialect == serverdb.DialectMySQL {
		clear += `, default_marker = NULL, fallback_marker = NULL`
	}
	if _, err := tx.ExecContext(ctx, clear); err != nil {
		return err
	}
	now := formatTime(time.Now().UTC())
	if defaultID != nil && *defaultID != "" {
		query := `UPDATE ai_provider_models SET is_default = 1, updated_at = ? WHERE id = ?`
		if s.dialect == serverdb.DialectMySQL {
			query = `UPDATE ai_provider_models SET is_default = 1, default_marker = '1', updated_at = ? WHERE id = ?`
		}
		if _, err := tx.ExecContext(ctx, query, now, *defaultID); err != nil {
			return err
		}
	}
	if fallbackID != nil && *fallbackID != "" {
		query := `UPDATE ai_provider_models SET is_fallback = 1, updated_at = ? WHERE id = ?`
		if s.dialect == serverdb.DialectMySQL {
			query = `UPDATE ai_provider_models SET is_fallback = 1, fallback_marker = '1', updated_at = ? WHERE id = ?`
		}
		if _, err := tx.ExecContext(ctx, query, now, *fallbackID); err != nil {
			return err
		}
	}
	providerIDs, err := providerIDsTx(ctx, tx)
	if err != nil {
		return err
	}
	for _, providerID := range providerIDs {
		if err := s.syncProviderCompatibilityTx(ctx, tx, providerID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *AIStore) DefaultModel(ctx context.Context) (AIProviderModel, error) {
	return scanAIProviderModel(s.db.QueryRowContext(ctx, aiProviderModelSelect+`
		JOIN ai_providers p ON p.id = ai_provider_models.provider_id
		WHERE ai_provider_models.is_default = 1 AND ai_provider_models.enabled = 1 AND
		 ai_provider_models.chat_capable = 1 AND ai_provider_models.tools_capable = 1 AND p.enabled = 1 LIMIT 1`))
}

func (s *AIStore) FallbackModel(ctx context.Context, requestedModelID string) (AIProviderModel, error) {
	return scanAIProviderModel(s.db.QueryRowContext(ctx, aiProviderModelSelect+`
		JOIN ai_providers p ON p.id = ai_provider_models.provider_id
		WHERE ai_provider_models.is_fallback = 1 AND ai_provider_models.id <> ? AND
		 ai_provider_models.enabled = 1 AND ai_provider_models.chat_capable = 1 AND
		 ai_provider_models.tools_capable = 1 AND p.enabled = 1 LIMIT 1`, requestedModelID))
}

func (s *AIStore) CreateConversation(ctx context.Context, title string) (AIConversation, error) {
	return s.CreateConversationWithModel(ctx, title, nil)
}

func (s *AIStore) CreateConversationWithModel(ctx context.Context, title string, modelID *string) (AIConversation, error) {
	now := time.Now().UTC()
	conversation := AIConversation{ID: uuid.NewString(), Title: title, ModelID: modelID, CreatedAt: now, UpdatedAt: now}
	_, err := s.db.ExecContext(ctx, `INSERT INTO ai_conversations (id, title, model_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		conversation.ID, conversation.Title, pointerValue(modelID), formatTime(now), formatTime(now))
	return conversation, err
}

func (s *AIStore) GetConversation(ctx context.Context, id string) (AIConversation, error) {
	var conversation AIConversation
	var created, updated string
	var modelID sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT id, title, model_id, created_at, updated_at FROM ai_conversations WHERE id = ?`, id).
		Scan(&conversation.ID, &conversation.Title, &modelID, &created, &updated)
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
	conversation.ModelID = nullableStringPointer(modelID)
	return conversation, nil
}

func (s *AIStore) ListConversations(ctx context.Context, limit int) ([]AIConversation, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, title, model_id, created_at, updated_at FROM ai_conversations ORDER BY updated_at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]AIConversation, 0)
	for rows.Next() {
		var item AIConversation
		var modelID sql.NullString
		var created, updated string
		if err := rows.Scan(&item.ID, &item.Title, &modelID, &created, &updated); err != nil {
			return nil, err
		}
		if item.CreatedAt, err = parseTime(created); err != nil {
			return nil, err
		}
		if item.UpdatedAt, err = parseTime(updated); err != nil {
			return nil, err
		}
		item.ModelID = nullableStringPointer(modelID)
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *AIStore) SetConversationModel(ctx context.Context, id string, modelID *string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if modelID != nil && *modelID != "" {
		var providerEnabled, enabled, chat, tools bool
		if err := tx.QueryRowContext(ctx, `SELECT p.enabled, m.enabled, m.chat_capable, m.tools_capable
			FROM ai_provider_models m JOIN ai_providers p ON p.id = m.provider_id WHERE m.id = ?`, *modelID).
			Scan(&providerEnabled, &enabled, &chat, &tools); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrAINotFound
			}
			return err
		}
		if !providerEnabled || !enabled || !chat || !tools {
			return ErrAIInvalid
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE ai_conversations SET model_id = ?, updated_at = ? WHERE id = ?`,
		pointerValue(modelID), formatTime(time.Now().UTC()), id)
	if err != nil {
		return err
	}
	if rows, err := result.RowsAffected(); err != nil {
		return err
	} else if rows == 0 {
		return ErrAINotFound
	}
	return tx.Commit()
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
	model, err := s.compatibilityModel(ctx, provider.ID)
	if err != nil {
		return AITurn{}, AIMessage{}, err
	}
	return s.StartModelTurn(ctx, conversationID, provider, model, userContent)
}

func (s *AIStore) StartModelTurn(ctx context.Context, conversationID string, provider AIProvider, model AIProviderModel, userContent string) (AITurn, AIMessage, error) {
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
	modelID := model.ID
	turn := AITurn{ID: uuid.NewString(), ConversationID: conversationID, ModelID: &modelID, ProviderID: provider.ID,
		ProviderName: provider.Name, Protocol: provider.Protocol, Model: model.ModelID,
		RequestedProviderID: provider.ID, RequestedProviderName: provider.Name,
		RequestedModelID: &modelID, RequestedModel: model.ModelID,
		Status: "running", CreatedAt: now, UpdatedAt: now}
	if s.dialect == serverdb.DialectMySQL {
		_, err = tx.ExecContext(ctx, `INSERT INTO ai_turns
			(id, conversation_id, model_id, provider_id, provider_name, protocol, model,
			 requested_provider_id, requested_provider_name, requested_model_id, requested_model,
			 fallback_used, status, error_code, active_marker, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, 'running', '', ?, ?, ?)`, turn.ID, conversationID,
			model.ID, provider.ID, provider.Name, provider.Protocol, model.ModelID, provider.ID, provider.Name,
			model.ID, model.ModelID, "1", formatTime(now), formatTime(now))
	} else {
		_, err = tx.ExecContext(ctx, `INSERT INTO ai_turns
			(id, conversation_id, model_id, provider_id, provider_name, protocol, model,
			 requested_provider_id, requested_provider_name, requested_model_id, requested_model,
			 fallback_used, status, error_code, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, 'running', '', ?, ?)`, turn.ID, conversationID,
			model.ID, provider.ID, provider.Name, provider.Protocol, model.ModelID, provider.ID, provider.Name,
			model.ID, model.ModelID, formatTime(now), formatTime(now))
	}
	if err != nil {
		if isUniqueError(err) {
			return AITurn{}, AIMessage{}, ErrAIConflict
		}
		return AITurn{}, AIMessage{}, err
	}
	message := AIMessage{ID: uuid.NewString(), ConversationID: conversationID, TurnID: turn.ID,
		Role: "user", Content: userContent, ProviderName: provider.Name, Model: model.ModelID, CreatedAt: now}
	if _, err := tx.ExecContext(ctx, `INSERT INTO ai_messages
		(id, conversation_id, turn_id, role, content, provider_name, model, created_at)
		VALUES (?, ?, ?, 'user', ?, ?, ?, ?)`, message.ID, conversationID, turn.ID, userContent,
		provider.Name, model.ModelID, formatTime(now)); err != nil {
		return AITurn{}, AIMessage{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE ai_conversations SET updated_at = ? WHERE id = ?`, formatTime(now), conversationID); err != nil {
		return AITurn{}, AIMessage{}, err
	}
	return turn, message, tx.Commit()
}

func (s *AIStore) SwitchTurnModelBeforeTools(ctx context.Context, turnID string, provider AIProvider, model AIProviderModel) (AITurn, error) {
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, `UPDATE ai_turns SET model_id = ?, provider_id = ?, provider_name = ?,
		protocol = ?, model = ?, fallback_used = 1, updated_at = ?
		WHERE id = ? AND status = 'running' AND fallback_used = 0
		AND NOT EXISTS (SELECT 1 FROM ai_tool_calls WHERE turn_id = ai_turns.id)`, model.ID, provider.ID,
		provider.Name, provider.Protocol, model.ModelID, formatTime(now), turnID)
	if err != nil {
		return AITurn{}, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return AITurn{}, err
	}
	if rows == 0 {
		return AITurn{}, ErrAIConflict
	}
	return s.GetTurn(ctx, turnID)
}

func (s *AIStore) GetTurn(ctx context.Context, id string) (AITurn, error) {
	return scanAITurn(s.db.QueryRowContext(ctx, aiTurnSelect+` WHERE id = ?`, id))
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
		c.result_summary, c.created_at, c.updated_at, t.conversation_id, t.model_id, t.provider_id,
		t.provider_name, t.protocol, t.model, t.requested_provider_id, t.requested_provider_name,
		t.requested_model_id, t.requested_model, t.fallback_used, t.status, t.error_code, t.created_at, t.updated_at
		FROM ai_tool_calls c JOIN ai_turns t ON t.id = c.turn_id WHERE c.id = ?`, id)
	var call AIToolCall
	var turn AITurn
	var modelID, requestedModelID sql.NullString
	var callCreated, callUpdated, turnCreated, turnUpdated string
	err := row.Scan(&call.ID, &call.TurnID, &call.ProviderCallID, &call.ToolName, &call.Risk,
		&call.Status, &call.ArgumentsJSON, &call.TargetType, &call.TargetID, &call.TargetName,
		&call.NodeID, &call.ResultSummary, &callCreated, &callUpdated, &turn.ConversationID, &modelID,
		&turn.ProviderID, &turn.ProviderName, &turn.Protocol, &turn.Model, &turn.RequestedProviderID,
		&turn.RequestedProviderName, &requestedModelID, &turn.RequestedModel, &turn.FallbackUsed, &turn.Status,
		&turn.ErrorCode, &turnCreated, &turnUpdated)
	if errors.Is(err, sql.ErrNoRows) {
		return AIToolCall{}, AITurn{}, ErrAINotFound
	}
	if err != nil {
		return AIToolCall{}, AITurn{}, err
	}
	turn.ID = call.TurnID
	turn.ModelID = nullableStringPointer(modelID)
	turn.RequestedModelID = nullableStringPointer(requestedModelID)
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
	enabled, discovery_status, discovery_latency_ms, discovered_at, discovery_error,
	is_default, chat_capable, tools_capable, probe_status, probed_at, probe_error, created_at, updated_at
	FROM ai_providers`

const aiProviderModelSelect = `SELECT ai_provider_models.id, ai_provider_models.provider_id,
	ai_provider_models.model_id, ai_provider_models.display_name, ai_provider_models.enabled,
	ai_provider_models.chat_capable, ai_provider_models.tools_capable, ai_provider_models.probe_status,
	ai_provider_models.probe_latency_ms, ai_provider_models.probed_at, ai_provider_models.probe_error,
	ai_provider_models.is_default, ai_provider_models.is_fallback, ai_provider_models.created_at,
	ai_provider_models.updated_at FROM ai_provider_models`

const aiTurnSelect = `SELECT id, conversation_id, model_id, provider_id, provider_name, protocol, model,
	requested_provider_id, requested_provider_name, requested_model_id, requested_model, fallback_used,
	status, error_code, created_at, updated_at FROM ai_turns`

type aiScanner interface {
	Scan(dest ...any) error
}

func scanAIProvider(scanner aiScanner) (AIProvider, error) {
	var provider AIProvider
	var discovered, probed sql.NullString
	var created, updated string
	err := scanner.Scan(&provider.ID, &provider.Name, &provider.Protocol, &provider.BaseURL,
		&provider.Model, &provider.APIKeyCiphertext, &provider.Enabled, &provider.DiscoveryStatus,
		&provider.DiscoveryLatency, &discovered, &provider.DiscoveryError, &provider.Default, &provider.ChatCapable,
		&provider.ToolsCapable, &provider.ProbeStatus, &probed, &provider.ProbeError,
		&created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return AIProvider{}, ErrAINotFound
	}
	if err != nil {
		return AIProvider{}, err
	}
	provider.HasAPIKey = provider.APIKeyCiphertext != ""
	if discovered.Valid && discovered.String != "" {
		value, err := parseTime(discovered.String)
		if err != nil {
			return AIProvider{}, err
		}
		provider.DiscoveredAt = &value
	}
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

func scanAIProviderModel(scanner aiScanner) (AIProviderModel, error) {
	var model AIProviderModel
	var probed sql.NullString
	var created, updated string
	err := scanner.Scan(&model.ID, &model.ProviderID, &model.ModelID, &model.DisplayName, &model.Enabled,
		&model.ChatCapable, &model.ToolsCapable, &model.ProbeStatus, &model.ProbeLatency, &probed,
		&model.ProbeError, &model.Default, &model.Fallback, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return AIProviderModel{}, ErrAINotFound
	}
	if err != nil {
		return AIProviderModel{}, err
	}
	if probed.Valid && probed.String != "" {
		value, err := parseTime(probed.String)
		if err != nil {
			return AIProviderModel{}, err
		}
		model.ProbedAt = &value
	}
	if model.CreatedAt, err = parseTime(created); err != nil {
		return AIProviderModel{}, err
	}
	if model.UpdatedAt, err = parseTime(updated); err != nil {
		return AIProviderModel{}, err
	}
	return model, nil
}

func scanAITurn(scanner aiScanner) (AITurn, error) {
	var turn AITurn
	var modelID, requestedModelID sql.NullString
	var created, updated string
	err := scanner.Scan(&turn.ID, &turn.ConversationID, &modelID, &turn.ProviderID, &turn.ProviderName,
		&turn.Protocol, &turn.Model, &turn.RequestedProviderID, &turn.RequestedProviderName,
		&requestedModelID, &turn.RequestedModel, &turn.FallbackUsed, &turn.Status, &turn.ErrorCode,
		&created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return AITurn{}, ErrAINotFound
	}
	if err != nil {
		return AITurn{}, err
	}
	turn.ModelID = nullableStringPointer(modelID)
	turn.RequestedModelID = nullableStringPointer(requestedModelID)
	if turn.CreatedAt, err = parseTime(created); err != nil {
		return AITurn{}, err
	}
	if turn.UpdatedAt, err = parseTime(updated); err != nil {
		return AITurn{}, err
	}
	return turn, nil
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

func (s *AIStore) compatibilityModel(ctx context.Context, providerID string) (AIProviderModel, error) {
	return scanAIProviderModel(s.db.QueryRowContext(ctx, aiProviderModelSelect+`
		WHERE provider_id = ? ORDER BY is_default DESC, enabled DESC, created_at, id LIMIT 1`, providerID))
}

func (s *AIStore) syncProviderCompatibilityTx(ctx context.Context, tx *sql.Tx, providerID string) error {
	row := tx.QueryRowContext(ctx, `SELECT model_id, chat_capable, tools_capable, probe_status,
		probed_at, probe_error, is_default FROM ai_provider_models WHERE provider_id = ?
		ORDER BY is_default DESC, enabled DESC, created_at, id LIMIT 1`, providerID)
	var model, probeStatus, probeError string
	var chat, tools, isDefault bool
	var probed sql.NullString
	err := row.Scan(&model, &chat, &tools, &probeStatus, &probed, &probeError, &isDefault)
	if errors.Is(err, sql.ErrNoRows) {
		query := `UPDATE ai_providers SET model = '', is_default = 0, chat_capable = 0,
			tools_capable = 0, probe_status = 'unknown', probed_at = NULL, probe_error = '' WHERE id = ?`
		if s.dialect == serverdb.DialectMySQL {
			query = `UPDATE ai_providers SET model = '', is_default = 0, default_marker = NULL,
				chat_capable = 0, tools_capable = 0, probe_status = 'unknown', probed_at = NULL,
				probe_error = '' WHERE id = ?`
		}
		_, err = tx.ExecContext(ctx, query, providerID)
		return err
	}
	if err != nil {
		return err
	}
	query := `UPDATE ai_providers SET model = ?, is_default = ?, chat_capable = ?, tools_capable = ?,
		probe_status = ?, probed_at = ?, probe_error = ? WHERE id = ?`
	arguments := []any{model, isDefault, chat, tools, probeStatus, nullableDatabaseValue(probed), probeError, providerID}
	if s.dialect == serverdb.DialectMySQL {
		query = `UPDATE ai_providers SET model = ?, is_default = ?, default_marker = ?, chat_capable = ?,
			tools_capable = ?, probe_status = ?, probed_at = ?, probe_error = ? WHERE id = ?`
		arguments = []any{model, isDefault, aiDefaultMarker(isDefault), chat, tools, probeStatus,
			nullableDatabaseValue(probed), probeError, providerID}
	}
	_, err = tx.ExecContext(ctx, query, arguments...)
	return err
}

func providerIDsTx(ctx context.Context, tx *sql.Tx) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM ai_providers`)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	return ids, rows.Close()
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

func boundedLatency(value int) int {
	if value < 0 {
		return 0
	}
	if value > 600000 {
		return 600000
	}
	return value
}

func stringPointer(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func nullableStringPointer(value sql.NullString) *string {
	if !value.Valid || value.String == "" {
		return nil
	}
	return stringPointer(value.String)
}

func pointerValue(value *string) any {
	if value == nil || *value == "" {
		return nil
	}
	return *value
}

func nullableDatabaseValue(value sql.NullString) any {
	if !value.Valid || value.String == "" {
		return nil
	}
	return value.String
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
