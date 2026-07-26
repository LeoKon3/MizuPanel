package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mizupanel/mizupanel/internal/server/store"
)

var validMetricFields = map[string]bool{
	"cpu_usage":        true,
	"memory_usage":     true,
	"disk_usage":       true,
	"swap_usage":       true,
	"network_rx_bytes": true,
	"network_tx_bytes": true,
	"load_1":           true,
	"load_5":           true,
	"load_15":          true,
}

var validOperators = map[string]bool{
	">":  true,
	">=": true,
	"<":  true,
	"<=": true,
	"=":  true,
}

func (s *Server) handleAlertRules(alertStore *store.AlertStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			rules, err := alertStore.GetAlertRules()
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			if rules == nil {
				rules = []store.AlertRule{}
			}
			writeJSON(w, http.StatusOK, map[string]any{"rules": rules})
		case http.MethodPost:
			markAudit(r, "alert", "rule_create", "alert_rule", "", "")
			if !sameOrigin(r) {
				writeError(w, http.StatusForbidden, "forbidden")
				return
			}
			var rule store.AlertRule
			if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
				writeError(w, http.StatusBadRequest, "invalid request body")
				return
			}
			if !validMetricFields[rule.MetricField] {
				writeError(w, http.StatusBadRequest, "invalid metric_field")
				return
			}
			if !validOperators[rule.Operator] {
				writeError(w, http.StatusBadRequest, "invalid operator")
				return
			}
			if err := alertStore.CreateAlertRule(&rule); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			setAuditTarget(r, "alert_rule", strconv.FormatInt(rule.ID, 10), rule.Name)
			setAuditMetadata(r, "enabled", strconv.FormatBool(rule.Enabled))
			writeJSON(w, http.StatusCreated, rule)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}
}

func (s *Server) handleAlertRuleRoutes(alertStore *store.AlertStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/alerts/rules/")
		parts := strings.Split(strings.Trim(path, "/"), "/")

		if len(parts) == 2 && parts[1] == "toggle" {
			s.handleToggleAlertRule(w, r, parts[0], alertStore)
			return
		}

		if len(parts) == 1 && parts[0] != "" {
			s.handleAlertRule(w, r, parts[0], alertStore)
			return
		}

		http.NotFound(w, r)
	}
}

func (s *Server) handleAlertRule(w http.ResponseWriter, r *http.Request, idStr string, alertStore *store.AlertStore) {
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid rule id")
		return
	}

	switch r.Method {
	case http.MethodGet:
		rule, err := alertStore.GetAlertRule(id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if rule == nil {
			writeError(w, http.StatusNotFound, "rule not found")
			return
		}
		writeJSON(w, http.StatusOK, rule)
	case http.MethodPut:
		markAudit(r, "alert", "rule_update", "alert_rule", idStr, "")
		if !sameOrigin(r) {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		existingRule, err := alertStore.GetAlertRule(id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if existingRule == nil {
			writeError(w, http.StatusNotFound, "rule not found")
			return
		}
		var rule store.AlertRule
		if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		rule.ID = id
		if !validMetricFields[rule.MetricField] {
			writeError(w, http.StatusBadRequest, "invalid metric_field")
			return
		}
		if !validOperators[rule.Operator] {
			writeError(w, http.StatusBadRequest, "invalid operator")
			return
		}
		if err := alertStore.UpdateAlertRule(&rule); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if existingRule.Name != rule.Name {
			if _, err := alertStore.UpdateAlertHistoryRuleNameByRuleID(rule.ID, rule.Name); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
		if !rule.Enabled {
			if _, err := alertStore.ResolveActiveAlertHistoryByRuleID(rule.ID, time.Now().UTC()); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
		setAuditTarget(r, "alert_rule", idStr, rule.Name)
		setAuditMetadata(r, "enabled", strconv.FormatBool(rule.Enabled))
		writeJSON(w, http.StatusOK, rule)
	case http.MethodDelete:
		markAudit(r, "alert", "rule_delete", "alert_rule", idStr, "")
		if !sameOrigin(r) {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		rule, err := alertStore.GetAlertRule(id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if rule == nil {
			writeError(w, http.StatusNotFound, "rule not found")
			return
		}
		setAuditTarget(r, "alert_rule", idStr, rule.Name)
		if _, err := alertStore.ResolveActiveAlertHistoryByRuleID(id, time.Now().UTC()); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := alertStore.DeleteAlertRule(id); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleToggleAlertRule(w http.ResponseWriter, r *http.Request, idStr string, alertStore *store.AlertStore) {
	if r.Method != http.MethodPatch {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	markAudit(r, "alert", "rule_toggle", "alert_rule", idStr, "")
	if !sameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid rule id")
		return
	}

	rule, err := alertStore.GetAlertRule(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if rule == nil {
		writeError(w, http.StatusNotFound, "rule not found")
		return
	}

	var request struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	setAuditTarget(r, "alert_rule", idStr, rule.Name)
	setAuditMetadata(r, "enabled", strconv.FormatBool(request.Enabled))

	rule.Enabled = request.Enabled
	if err := alertStore.UpdateAlertRule(rule); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !rule.Enabled {
		if _, err := alertStore.ResolveActiveAlertHistoryByRuleID(rule.ID, time.Now().UTC()); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	writeJSON(w, http.StatusOK, rule)
}
