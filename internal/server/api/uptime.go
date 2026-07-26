package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/mizupanel/mizupanel/internal/server/store"
	serveruptime "github.com/mizupanel/mizupanel/internal/server/uptime"
)

const maxUptimeRequestBodyBytes = 256 * 1024

type uptimeMonitorRequest struct {
	Name                   string                      `json:"name"`
	Type                   string                      `json:"type"`
	Target                 string                      `json:"target"`
	Enabled                *bool                       `json:"enabled,omitempty"`
	IntervalSeconds        int                         `json:"interval_seconds"`
	TimeoutSeconds         int                         `json:"timeout_seconds"`
	FailureThreshold       int                         `json:"failure_threshold"`
	ExpectedStatusMin      int                         `json:"expected_status_min"`
	ExpectedStatusMax      int                         `json:"expected_status_max"`
	TLSExpiryThresholdDays int                         `json:"tls_expiry_threshold_days"`
	NotificationChannels   []store.NotificationChannel `json:"notification_channels"`
}

func (request uptimeMonitorRequest) monitor(defaultEnabled bool) store.UptimeMonitor {
	enabled := defaultEnabled
	if request.Enabled != nil {
		enabled = *request.Enabled
	}
	return store.UptimeMonitor{
		Name:                   request.Name,
		Type:                   request.Type,
		Target:                 request.Target,
		Enabled:                enabled,
		IntervalSeconds:        request.IntervalSeconds,
		TimeoutSeconds:         request.TimeoutSeconds,
		FailureThreshold:       request.FailureThreshold,
		ExpectedStatusMin:      request.ExpectedStatusMin,
		ExpectedStatusMax:      request.ExpectedStatusMax,
		TLSExpiryThresholdDays: request.TLSExpiryThresholdDays,
		NotificationChannels:   request.NotificationChannels,
	}
}

func (s *Server) handleUptimeMonitors(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		monitors, err := s.uptime.ListMonitors(r.Context())
		if err != nil {
			log.Printf("list uptime monitors failed: %v", err)
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"monitors": monitors})
	case http.MethodPost:
		markAudit(r, "uptime", "monitor_create", "uptime_monitor", "", "")
		if !authorizeUptimeMutation(r, true) {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		var request uptimeMonitorRequest
		if !decodeUptimeRequest(w, r, &request) {
			return
		}
		monitor := request.monitor(true)
		if err := serveruptime.ValidateMonitor(&monitor); err != nil {
			writeUptimeError(w, err)
			return
		}
		if err := s.uptime.CreateMonitor(r.Context(), &monitor); err != nil {
			log.Printf("create uptime monitor failed: %v", err)
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		setAuditTarget(r, "uptime_monitor", strconv.FormatInt(monitor.ID, 10), monitor.Name)
		setAuditMetadata(r, "enabled", strconv.FormatBool(monitor.Enabled))
		writeJSON(w, http.StatusCreated, monitor)
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func (s *Server) handleUptimeMonitorRoutes(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/uptime/monitors/"), "/")
	if path == "" {
		http.NotFound(w, r)
		return
	}
	parts := strings.Split(path, "/")
	monitorID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || monitorID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid monitor id")
		return
	}
	if len(parts) == 1 {
		s.handleUptimeMonitor(w, r, monitorID)
		return
	}
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	switch parts[1] {
	case "toggle":
		s.handleUptimeToggle(w, r, monitorID)
	case "check":
		s.handleUptimeCheck(w, r, monitorID)
	case "results":
		s.handleUptimeResults(w, r, monitorID)
	case "incidents":
		s.handleUptimeIncidents(w, r, monitorID)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleUptimeMonitor(w http.ResponseWriter, r *http.Request, monitorID int64) {
	switch r.Method {
	case http.MethodGet:
		monitor, err := s.uptime.GetMonitor(r.Context(), monitorID)
		if !writeUptimeMonitorLookup(w, monitor, err, "get", monitorID) {
			return
		}
		writeJSON(w, http.StatusOK, monitor)
	case http.MethodPut:
		markAudit(r, "uptime", "monitor_update", "uptime_monitor", strconv.FormatInt(monitorID, 10), "")
		if !authorizeUptimeMutation(r, true) {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		release, ok := s.beginUptimeMonitorMutation(w, monitorID)
		if !ok {
			return
		}
		defer release()
		existing, err := s.uptime.GetMonitor(r.Context(), monitorID)
		if !writeUptimeMonitorLookup(w, existing, err, "get for update", monitorID) {
			return
		}
		setAuditTarget(r, "uptime_monitor", strconv.FormatInt(monitorID, 10), existing.Name)
		var request uptimeMonitorRequest
		if !decodeUptimeRequest(w, r, &request) {
			return
		}
		monitor := request.monitor(existing.Enabled)
		monitor.ID = monitorID
		if err := serveruptime.ValidateMonitor(&monitor); err != nil {
			writeUptimeError(w, err)
			return
		}
		if _, err := s.uptime.UpdateMonitor(r.Context(), &monitor); err != nil {
			writeUptimeStoreError(w, err, "update", monitorID)
			return
		}
		if request.Enabled != nil && monitor.Enabled != *request.Enabled {
			updated, err := s.uptime.SetMonitorEnabled(r.Context(), monitorID, *request.Enabled)
			if err != nil {
				writeUptimeStoreError(w, err, "set enabled after update", monitorID)
				return
			}
			monitor = *updated
		}
		setAuditTarget(r, "uptime_monitor", strconv.FormatInt(monitorID, 10), monitor.Name)
		setAuditMetadata(r, "enabled", strconv.FormatBool(monitor.Enabled))
		writeJSON(w, http.StatusOK, monitor)
	case http.MethodDelete:
		markAudit(r, "uptime", "monitor_delete", "uptime_monitor", strconv.FormatInt(monitorID, 10), "")
		if !authorizeUptimeMutation(r, false) {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		release, ok := s.beginUptimeMonitorMutation(w, monitorID)
		if !ok {
			return
		}
		defer release()
		deleted, err := s.uptime.DeleteMonitor(r.Context(), monitorID)
		if err != nil {
			writeUptimeStoreError(w, err, "delete", monitorID)
			return
		}
		if !deleted {
			writeError(w, http.StatusNotFound, "monitor not found")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodPut, http.MethodDelete)
	}
}

func (s *Server) handleUptimeToggle(w http.ResponseWriter, r *http.Request, monitorID int64) {
	if r.Method != http.MethodPatch {
		writeMethodNotAllowed(w, http.MethodPatch)
		return
	}
	markAudit(r, "uptime", "monitor_toggle", "uptime_monitor", strconv.FormatInt(monitorID, 10), "")
	if !authorizeUptimeMutation(r, true) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	var request struct {
		Enabled *bool `json:"enabled"`
	}
	if !decodeUptimeRequest(w, r, &request) {
		return
	}
	if request.Enabled == nil {
		writeError(w, http.StatusBadRequest, "enabled is required")
		return
	}
	release, ok := s.beginUptimeMonitorMutation(w, monitorID)
	if !ok {
		return
	}
	defer release()
	monitor, err := s.uptime.SetMonitorEnabled(r.Context(), monitorID, *request.Enabled)
	if err != nil {
		writeUptimeStoreError(w, err, "toggle", monitorID)
		return
	}
	setAuditTarget(r, "uptime_monitor", strconv.FormatInt(monitorID, 10), monitor.Name)
	setAuditMetadata(r, "enabled", strconv.FormatBool(monitor.Enabled))
	writeJSON(w, http.StatusOK, monitor)
}

func (s *Server) handleUptimeCheck(w http.ResponseWriter, r *http.Request, monitorID int64) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	markAudit(r, "uptime", "monitor_check", "uptime_monitor", strconv.FormatInt(monitorID, 10), "")
	if !authorizeUptimeMutation(r, false) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	if s.uptimeChecker == nil {
		writeError(w, http.StatusServiceUnavailable, "uptime checker is not available")
		return
	}
	monitor, err := s.uptimeChecker.CheckNow(r.Context(), monitorID)
	if err != nil {
		switch {
		case errors.Is(err, serveruptime.ErrMonitorNotFound), errors.Is(err, sql.ErrNoRows):
			writeError(w, http.StatusNotFound, "monitor not found")
		case errors.Is(err, serveruptime.ErrCheckInProgress):
			writeError(w, http.StatusConflict, "monitor check already in progress")
		case errors.Is(err, serveruptime.ErrMonitorDisabled):
			writeError(w, http.StatusConflict, "monitor is disabled")
		default:
			log.Printf("check uptime monitor %d failed: %v", monitorID, err)
			writeError(w, http.StatusInternalServerError, "internal server error")
		}
		return
	}
	setAuditTarget(r, "uptime_monitor", strconv.FormatInt(monitorID, 10), monitor.Name)
	writeJSON(w, http.StatusOK, monitor)
}

func (s *Server) beginUptimeMonitorMutation(w http.ResponseWriter, monitorID int64) (func(), bool) {
	if s.uptimeChecker == nil {
		return func() {}, true
	}
	release, err := s.uptimeChecker.BeginMonitorMutation(monitorID)
	if err == nil {
		return release, true
	}
	if errors.Is(err, serveruptime.ErrCheckInProgress) {
		writeError(w, http.StatusConflict, "monitor check or update already in progress")
		return nil, false
	}
	log.Printf("reserve uptime monitor %d mutation failed: %v", monitorID, err)
	writeError(w, http.StatusInternalServerError, "internal server error")
	return nil, false
}

func (s *Server) handleUptimeResults(w http.ResponseWriter, r *http.Request, monitorID int64) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	if !s.uptimeMonitorExists(w, r, monitorID) {
		return
	}
	limit, ok := uptimeHistoryLimit(w, r, store.MaxUptimeResults)
	if !ok {
		return
	}
	results, err := s.uptime.ListResults(r.Context(), monitorID, limit)
	if err != nil {
		log.Printf("list uptime monitor %d results failed: %v", monitorID, err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

func (s *Server) handleUptimeIncidents(w http.ResponseWriter, r *http.Request, monitorID int64) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	if !s.uptimeMonitorExists(w, r, monitorID) {
		return
	}
	limit, ok := uptimeHistoryLimit(w, r, store.MaxUptimeIncidents)
	if !ok {
		return
	}
	incidents, err := s.uptime.ListIncidents(r.Context(), monitorID, limit)
	if err != nil {
		log.Printf("list uptime monitor %d incidents failed: %v", monitorID, err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"incidents": incidents})
}

func (s *Server) uptimeMonitorExists(w http.ResponseWriter, r *http.Request, monitorID int64) bool {
	monitor, err := s.uptime.GetMonitor(r.Context(), monitorID)
	return writeUptimeMonitorLookup(w, monitor, err, "get for history", monitorID)
}

func writeUptimeMonitorLookup(w http.ResponseWriter, monitor *store.UptimeMonitor, err error, operation string, monitorID int64) bool {
	if err != nil {
		log.Printf("%s uptime monitor %d failed: %v", operation, monitorID, err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return false
	}
	if monitor == nil {
		writeError(w, http.StatusNotFound, "monitor not found")
		return false
	}
	return true
}

func writeUptimeStoreError(w http.ResponseWriter, err error, operation string, monitorID int64) {
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "monitor not found")
		return
	}
	log.Printf("%s uptime monitor %d failed: %v", operation, monitorID, err)
	writeError(w, http.StatusInternalServerError, "internal server error")
}

func writeUptimeError(w http.ResponseWriter, err error) {
	if errors.Is(err, serveruptime.ErrInvalidMonitor) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeError(w, http.StatusInternalServerError, "internal server error")
}

func decodeUptimeRequest(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxUptimeRequestBodyBytes)
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
		writeError(w, http.StatusBadRequest, "request body must contain one JSON object")
		return false
	}
	return true
}

func authorizeUptimeMutation(r *http.Request, requireJSON bool) bool {
	if !sameOrigin(r) {
		return false
	}
	if !requireJSON {
		return true
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return err == nil && strings.EqualFold(mediaType, "application/json")
}

func uptimeHistoryLimit(w http.ResponseWriter, r *http.Request, maximum int) (int, bool) {
	value := strings.TrimSpace(r.URL.Query().Get("limit"))
	if value == "" {
		if maximum < 50 {
			return maximum, true
		}
		return 50, true
	}
	limit, err := strconv.Atoi(value)
	if err != nil || limit < 1 || limit > maximum {
		writeError(w, http.StatusBadRequest, "invalid limit")
		return 0, false
	}
	return limit, true
}

func writeMethodNotAllowed(w http.ResponseWriter, methods ...string) {
	w.Header().Set("Allow", strings.Join(methods, ", "))
	writeError(w, http.StatusMethodNotAllowed, "method not allowed")
}
