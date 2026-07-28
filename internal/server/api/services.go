package api

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/mizupanel/mizupanel/internal/server/servicecenter"
)

type ServiceCenter interface {
	List(ctx context.Context) ([]servicecenter.ServiceSummary, error)
	Get(ctx context.Context, id string) (servicecenter.ServiceDetail, error)
	Definition(ctx context.Context, id string) (servicecenter.Service, error)
	Create(ctx context.Context, input servicecenter.ServiceInput) (servicecenter.ServiceDetail, error)
	Update(ctx context.Context, id string, input servicecenter.ServiceInput) (servicecenter.ServiceDetail, error)
	Delete(ctx context.Context, id string) error
}

type ServiceCenterConfig struct {
	Facade ServiceCenter
}

func (s *Server) handleServices(w http.ResponseWriter, r *http.Request) {
	if s.serviceCenter == nil {
		writeError(w, http.StatusServiceUnavailable, "应用服务中心不可用")
		return
	}
	switch r.Method {
	case http.MethodGet:
		services, err := s.serviceCenter.List(r.Context())
		if err != nil {
			log.Printf("list application services failed")
			writeError(w, http.StatusInternalServerError, "读取应用服务失败")
			return
		}
		writeJSON(w, http.StatusOK, services)
	case http.MethodPost:
		markAudit(r, "service", "create", "application_service", "", "")
		if !authorizeAutomationMutation(w, r, true) {
			return
		}
		var input servicecenter.ServiceInput
		if !decodeAutomationRequest(w, r, &input) {
			return
		}
		service, err := s.serviceCenter.Create(r.Context(), input)
		if err != nil {
			writeServiceCenterError(w, err)
			return
		}
		setAuditTarget(r, "application_service", service.ID, service.Name)
		setAuditMetadata(r, "resource_count", strconv.Itoa(service.ResourceCount))
		writeJSON(w, http.StatusCreated, service)
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func (s *Server) handleServiceRoutes(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/services/"))
	if id == "" || strings.Contains(id, "/") {
		writeError(w, http.StatusNotFound, "应用服务不存在")
		return
	}
	if s.serviceCenter == nil {
		writeError(w, http.StatusServiceUnavailable, "应用服务中心不可用")
		return
	}
	switch r.Method {
	case http.MethodGet:
		service, err := s.serviceCenter.Get(r.Context(), id)
		if err != nil {
			writeServiceCenterError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, service)
	case http.MethodPut:
		markAudit(r, "service", "update", "application_service", id, "")
		if !authorizeAutomationMutation(w, r, true) {
			return
		}
		var input servicecenter.ServiceInput
		if !decodeAutomationRequest(w, r, &input) {
			return
		}
		service, err := s.serviceCenter.Update(r.Context(), id, input)
		if err != nil {
			writeServiceCenterError(w, err)
			return
		}
		setAuditTarget(r, "application_service", service.ID, service.Name)
		setAuditMetadata(r, "resource_count", strconv.Itoa(service.ResourceCount))
		writeJSON(w, http.StatusOK, service)
	case http.MethodDelete:
		markAudit(r, "service", "delete", "application_service", id, "")
		if !authorizeAutomationMutation(w, r, false) {
			return
		}
		definition, err := s.serviceCenter.Definition(r.Context(), id)
		if err != nil {
			writeServiceCenterError(w, err)
			return
		}
		setAuditTarget(r, "application_service", definition.ID, definition.Name)
		setAuditMetadata(r, "resource_count", strconv.Itoa(len(definition.Resources)))
		if err := s.serviceCenter.Delete(r.Context(), id); err != nil {
			writeServiceCenterError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodPut, http.MethodDelete)
	}
}

func writeServiceCenterError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, servicecenter.ErrInvalid):
		writeError(w, http.StatusBadRequest, publicServiceError(err, "应用服务数据无效"))
	case errors.Is(err, servicecenter.ErrConflict):
		writeError(w, http.StatusConflict, publicServiceError(err, "应用服务名称或资源关联已存在"))
	case errors.Is(err, servicecenter.ErrNotFound):
		writeError(w, http.StatusNotFound, "应用服务不存在")
	default:
		log.Printf("application service operation failed")
		writeError(w, http.StatusInternalServerError, "应用服务操作失败")
	}
}

func publicServiceError(err error, fallback string) string {
	message := err.Error()
	if index := strings.LastIndex(message, ": "); index >= 0 && index+2 < len(message) {
		message = message[index+2:]
	}
	message = strings.TrimSpace(message)
	if message == "" {
		return fallback
	}
	return message
}
