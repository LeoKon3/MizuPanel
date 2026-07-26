package api

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	serveraudit "github.com/mizupanel/mizupanel/internal/server/audit"
)

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
