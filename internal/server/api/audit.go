package api

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	serveraudit "github.com/mizupanel/mizupanel/internal/server/audit"
)

const (
	maxAuditCleanupBodyBytes = 4 * 1024
	maxAuditCleanupDays      = 3650
	minAuditCleanupAge       = 24 * time.Hour
)

type auditCleanupRequest struct {
	Before        json.RawMessage `json:"before"`
	OlderThanDays json.RawMessage `json:"older_than_days"`
}

type auditCleanupResponse struct {
	DeletedCount int64     `json:"deleted_count"`
	Cutoff       time.Time `json:"cutoff"`
}

func (s *Server) handleAuditEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	filter, err := parseAuditFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid audit filters")
		return
	}
	page, err := s.audit.List(r.Context(), filter)
	if err != nil {
		if errors.Is(err, serveraudit.ErrInvalid) {
			writeError(w, http.StatusBadRequest, "invalid audit filters")
			return
		}
		log.Printf("audit query failed request_id=%s class=storage_error", serveraudit.RequestID(r))
		writeError(w, http.StatusInternalServerError, "failed to load audit events")
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Events       []serveraudit.Event `json:"events"`
		NextBeforeID *int64              `json:"next_before_id"`
	}{Events: page.Events, NextBeforeID: page.NextBeforeID})
}

func (s *Server) handleAuditCleanup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	markAudit(r, "audit", "cleanup", "audit_events", "", "")
	if !sameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(mediaType, "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, "content type must be application/json")
		return
	}

	var request auditCleanupRequest
	if !decodeAuditCleanupRequest(w, r, &request) {
		return
	}
	now := time.Now().UTC()
	cutoff, err := auditCleanupCutoff(request, now)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid audit cleanup request")
		return
	}
	setAuditMetadata(r, "cutoff", cutoff.Format(time.RFC3339))

	deletedCount, err := s.audit.DeleteOlderThan(r.Context(), cutoff)
	if err != nil {
		log.Printf("audit cleanup failed request_id=%s class=storage_error", serveraudit.RequestID(r))
		writeError(w, http.StatusInternalServerError, "failed to clean audit events")
		return
	}
	setAuditMetadata(r, "deleted_count", strconv.FormatInt(deletedCount, 10))
	writeJSON(w, http.StatusOK, auditCleanupResponse{DeletedCount: deletedCount, Cutoff: cutoff})
}

func decodeAuditCleanupRequest(w http.ResponseWriter, r *http.Request, target *auditCleanupRequest) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxAuditCleanupBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
		} else {
			writeError(w, http.StatusBadRequest, "invalid request body")
		}
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
		} else {
			writeError(w, http.StatusBadRequest, "request body must contain one JSON object")
		}
		return false
	}
	return true
}

func auditCleanupCutoff(request auditCleanupRequest, now time.Time) (time.Time, error) {
	hasBefore := len(request.Before) > 0
	hasDays := len(request.OlderThanDays) > 0
	if hasBefore == hasDays {
		return time.Time{}, serveraudit.ErrInvalid
	}

	var cutoff time.Time
	if hasBefore {
		var before string
		if err := json.Unmarshal(request.Before, &before); err != nil {
			return time.Time{}, serveraudit.ErrInvalid
		}
		before = strings.TrimSpace(before)
		if before == "" {
			return time.Time{}, serveraudit.ErrInvalid
		}
		parsed, err := time.Parse(time.RFC3339, before)
		if err != nil {
			return time.Time{}, serveraudit.ErrInvalid
		}
		cutoff = parsed.UTC()
	} else {
		var days int
		if err := json.Unmarshal(request.OlderThanDays, &days); err != nil {
			return time.Time{}, serveraudit.ErrInvalid
		}
		if days < 1 || days > maxAuditCleanupDays {
			return time.Time{}, serveraudit.ErrInvalid
		}
		cutoff = now.UTC().Add(-time.Duration(days) * 24 * time.Hour)
	}
	if cutoff.After(now.UTC().Add(-minAuditCleanupAge)) {
		return time.Time{}, serveraudit.ErrInvalid
	}
	return cutoff, nil
}

func parseAuditFilter(r *http.Request) (serveraudit.Filter, error) {
	query := r.URL.Query()
	filter := serveraudit.Filter{
		ActorType: strings.TrimSpace(query.Get("actor_type")),
		ActorName: strings.TrimSpace(query.Get("actor_name")),
		Module:    strings.TrimSpace(query.Get("module")),
		Action:    strings.TrimSpace(query.Get("action")),
		NodeID:    strings.TrimSpace(query.Get("node_id")),
		Result:    strings.TrimSpace(query.Get("result")),
		Query:     strings.TrimSpace(query.Get("q")),
	}
	var err error
	if value := strings.TrimSpace(query.Get("before_id")); value != "" {
		filter.BeforeID, err = strconv.ParseInt(value, 10, 64)
		if err != nil || filter.BeforeID <= 0 {
			return serveraudit.Filter{}, serveraudit.ErrInvalid
		}
	}
	if value := strings.TrimSpace(query.Get("limit")); value != "" {
		filter.Limit, err = strconv.Atoi(value)
		if err != nil || filter.Limit <= 0 {
			return serveraudit.Filter{}, serveraudit.ErrInvalid
		}
	}
	if filter.From, err = parseAuditTime(query.Get("from")); err != nil {
		return serveraudit.Filter{}, err
	}
	if filter.To, err = parseAuditTime(query.Get("to")); err != nil {
		return serveraudit.Filter{}, err
	}
	return filter, nil
}

func parseAuditTime(value string) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, serveraudit.ErrInvalid
	}
	parsed = parsed.UTC()
	return &parsed, nil
}
