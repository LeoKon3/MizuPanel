package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mizupanel/mizupanel/internal/server/store"
)

func (s *Server) handleAlertHistory(alertStore *store.AlertStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			s.handleListAlertHistory(w, r, alertStore)
		case http.MethodDelete:
			s.handleDeleteAlertHistories(w, r, alertStore)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}
}

func (s *Server) handleListAlertHistory(w http.ResponseWriter, r *http.Request, alertStore *store.AlertStore) {
	nodeID := r.URL.Query().Get("node_id")
	if nodeID == "" {
		writeError(w, http.StatusBadRequest, "node_id is required")
		return
	}

	limit := 100
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 && parsed <= 1000 {
			limit = parsed
		}
	}

	if _, err := alertStore.ResolveActiveAlertHistoryForDisabledRules(time.Now().UTC()); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	history, err := alertStore.GetAlertHistory(nodeID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if history == nil {
		history = make([]store.AlertHistory, 0)
	}

	writeJSON(w, http.StatusOK, map[string]any{"history": history})
}

func (s *Server) handleAlertHistoryRoutes(alertStore *store.AlertStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/alerts/history/")
		parts := strings.Split(strings.Trim(path, "/"), "/")

		if len(parts) == 1 && parts[0] != "" {
			s.handleDeleteAlertHistory(w, r, parts[0], alertStore)
			return
		}

		if len(parts) == 2 && parts[1] == "resolve" {
			s.handleResolveAlertHistory(w, r, parts[0], alertStore)
			return
		}

		http.NotFound(w, r)
	}
}

func (s *Server) handleResolveAlertHistory(w http.ResponseWriter, r *http.Request, idStr string, alertStore *store.AlertStore) {
	if r.Method != http.MethodPatch {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	markAudit(r, "alert", "history_resolve", "alert_history", idStr, "")
	if !sameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid alert history id")
		return
	}

	history, err := alertStore.ResolveAlertHistory(id, time.Now().UTC())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if history == nil {
		writeError(w, http.StatusNotFound, "alert history not found")
		return
	}
	setAuditTarget(r, "alert_history", idStr, history.RuleName)
	setAuditNode(r, history.NodeID)

	writeJSON(w, http.StatusOK, history)
}

func (s *Server) handleDeleteAlertHistory(w http.ResponseWriter, r *http.Request, idStr string, alertStore *store.AlertStore) {
	if r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	markAudit(r, "alert", "history_delete", "alert_history", idStr, "")
	if !sameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid alert history id")
		return
	}

	history, err := alertStore.GetAlertHistoryByID(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if history == nil {
		writeError(w, http.StatusNotFound, "alert history not found")
		return
	}
	setAuditTarget(r, "alert_history", idStr, history.RuleName)
	setAuditNode(r, history.NodeID)
	if history.ResolvedAt == nil {
		writeError(w, http.StatusConflict, "active alert must be resolved before deletion")
		return
	}

	deleted, err := alertStore.DeleteResolvedAlertHistory(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !deleted {
		writeError(w, http.StatusConflict, "active alert must be resolved before deletion")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeleteAlertHistories(w http.ResponseWriter, r *http.Request, alertStore *store.AlertStore) {
	markAudit(r, "alert", "history_delete_bulk", "alert_history_batch", "", "")
	if !sameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	var request struct {
		IDs []int64 `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ids, ok := normalizeAlertHistoryIDs(request.IDs)
	if !ok {
		writeError(w, http.StatusBadRequest, "ids are required")
		return
	}
	setAuditMetadata(r, "requested_count", strconv.Itoa(len(ids)))

	for _, id := range ids {
		history, err := alertStore.GetAlertHistoryByID(id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if history == nil {
			writeError(w, http.StatusNotFound, "alert history not found")
			return
		}
		if history.ResolvedAt == nil {
			writeError(w, http.StatusConflict, "active alert must be resolved before deletion")
			return
		}
	}

	deleted, err := alertStore.DeleteResolvedAlertHistories(ids)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	setAuditMetadata(r, "deleted_count", strconv.FormatInt(deleted, 10))

	writeJSON(w, http.StatusOK, map[string]any{"deleted": deleted})
}

func normalizeAlertHistoryIDs(ids []int64) ([]int64, bool) {
	if len(ids) == 0 {
		return nil, false
	}

	seen := make(map[int64]struct{}, len(ids))
	normalized := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return nil, false
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		normalized = append(normalized, id)
	}
	return normalized, len(normalized) > 0
}
