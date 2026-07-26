package api

import (
	"net/http"

	serveraudit "github.com/mizupanel/mizupanel/internal/server/audit"
)

func markAudit(r *http.Request, module, action, targetType, targetID, nodeID string) {
	serveraudit.Mark(r, module, action)
	serveraudit.SetTarget(r, targetType, targetID, "")
	if nodeID != "" {
		serveraudit.SetNodeID(r, nodeID)
	}
}

func setAuditTarget(r *http.Request, targetType, targetID, targetName string) {
	serveraudit.SetTarget(r, targetType, targetID, targetName)
}

func setAuditAction(r *http.Request, action string) {
	serveraudit.SetAction(r, action)
}

func setAuditMetadata(r *http.Request, key, value string) {
	serveraudit.SetMetadata(r, key, value)
}

func setAuditNode(r *http.Request, nodeID string) {
	serveraudit.SetNodeID(r, nodeID)
}

func setAuditOutcome(r *http.Request, success bool) {
	if !success {
		serveraudit.SetResult(r, serveraudit.ResultFailure, "remote_operation_failed")
	}
}

func setAuditAccepted(r *http.Request, accepted bool) {
	if accepted {
		serveraudit.SetResult(r, serveraudit.ResultAccepted, "accepted")
		return
	}
	serveraudit.SetResult(r, serveraudit.ResultFailure, "remote_operation_failed")
}
