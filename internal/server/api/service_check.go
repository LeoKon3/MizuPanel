package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/mizupanel/mizupanel/internal/server/servicecenter"
	serveruptime "github.com/mizupanel/mizupanel/internal/server/uptime"
)

const maxServiceCheckMonitors = 20

type serviceProbeCheck struct {
	ResourceID string     `json:"resource_id"`
	Name       string     `json:"name"`
	Status     string     `json:"status"`
	Message    string     `json:"message"`
	CheckedAt  *time.Time `json:"checked_at,omitempty"`
}

type serviceHealthCheck struct {
	Service   servicecenter.ServiceDetail `json:"service"`
	Status    string                      `json:"status"`
	StartedAt time.Time                   `json:"started_at"`
	CheckedAt time.Time                   `json:"checked_at"`
	Checks    []serviceProbeCheck         `json:"checks"`
	Summary   string                      `json:"summary"`
}

// Check saved monitor IDs only. A request cannot supply probe destinations or
// change the service definition. Existing uptime checks own overlap protection,
// probe persistence and fault/recovery notifications.
func (s *Server) handleServiceHealthCheck(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	if !authorizeAutomationMutation(w, r, false) {
		return
	}
	if s.serviceCenter == nil {
		writeError(w, http.StatusServiceUnavailable, "应用服务中心不可用")
		return
	}
	definition, err := s.serviceCenter.Definition(r.Context(), id)
	if err != nil {
		writeServiceCenterError(w, err)
		return
	}
	markAudit(r, "service", "health_check", "application_service", id, definition.Name)
	result := serviceHealthCheck{StartedAt: time.Now().UTC(), Checks: []serviceProbeCheck{}}
	monitors := []servicecenter.Resource{}
	for _, resource := range definition.Resources {
		if resource.ResourceType == servicecenter.ResourceUptimeMonitor {
			monitors = append(monitors, resource)
			result.Checks = append(result.Checks, serviceProbeCheck{ResourceID: resource.ID, Name: resource.DisplayName, Status: "unavailable", Message: "未完成本次拨测"})
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	var workers sync.WaitGroup
	slots := make(chan struct{}, 4)
	for index, resource := range monitors {
		if index >= maxServiceCheckMonitors {
			result.Checks[index].Message = "本次最多复查 20 个拨测，请在拨测页面检查其余目标"
			continue
		}
		workers.Add(1)
		go func(index int, resource servicecenter.Resource) {
			defer workers.Done()
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
			case <-ctx.Done():
				return
			}
			if ctx.Err() != nil || s.uptimeChecker == nil {
				return
			}
			monitorID, parseErr := strconv.ParseInt(resource.ResourceKey, 10, 64)
			if parseErr != nil || monitorID <= 0 {
				return
			}
			monitor, checkErr := s.uptimeChecker.CheckNow(ctx, monitorID)
			check := &result.Checks[index]
			if checkErr != nil {
				switch {
				case errors.Is(checkErr, serveruptime.ErrCheckInProgress):
					check.Message = "已有拨测进行中，请稍后复查"
				case errors.Is(checkErr, serveruptime.ErrMonitorDisabled):
					check.Message = "拨测已禁用，未执行"
				case errors.Is(checkErr, serveruptime.ErrMonitorNotFound):
					check.Message = "关联拨测已不存在"
				default:
					check.Message = "拨测未完成，请在拨测页面检查"
				}
				return
			}
			if monitor.LastCheckedAt == nil || monitor.LastCheckedAt.Before(result.StartedAt) {
				check.Message = "未取得本次拨测结果"
				return
			}
			check.CheckedAt = monitor.LastCheckedAt
			switch monitor.Status {
			case "up":
				check.Status, check.Message = "healthy", "本次拨测通过"
			case "down", "warning":
				check.Status, check.Message = "issues", "本次拨测仍有异常或证书预警"
			}
		}(index, resource)
	}
	workers.Wait()
	if r.Context().Err() != nil {
		return
	}
	// Fresh remote resource reads follow the probes, with the facade's own bound.
	result.Service, err = s.serviceCenter.Get(r.Context(), id)
	if err != nil {
		writeServiceCenterError(w, err)
		return
	}
	result.CheckedAt = time.Now().UTC()
	result.Status = "healthy"
	if len(result.Checks) == 0 || len(result.Service.Resources) == 0 || result.Service.Health == servicecenter.HealthUnknown {
		result.Status = "inconclusive"
	}
	for _, check := range result.Checks {
		if check.Status == "unavailable" {
			result.Status = "inconclusive"
		}
	}
	for _, resource := range result.Service.Resources {
		if resource.Health == servicecenter.HealthUnknown || resource.State != "available" {
			result.Status = "inconclusive"
		}
	}
	if result.Service.Health == servicecenter.HealthUnhealthy || result.Service.Health == servicecenter.HealthDegraded {
		result.Status = "issues"
	}
	for _, check := range result.Checks {
		if check.Status == "issues" {
			result.Status = "issues"
		}
	}
	// Concurrent edits invalidate the set of resources this check evaluated.
	if !sameServiceCheckResources(definition.Resources, result.Service.Resources) {
		result.Status = "inconclusive"
		result.Summary = "关联资源在复查期间发生变化，请重新复查"
	} else {
		result.Summary = map[string]string{
			"healthy":      "关联资源当前健康，本次关联拨测全部通过",
			"issues":       "仍有异常或降级，请查看资源状态与拨测结果",
			"inconclusive": "证据不足：未关联拨测或部分状态不可用，尚不能确认恢复",
		}[result.Status]
	}
	setAuditMetadata(r, "resource_count", strconv.Itoa(len(result.Service.Resources)))
	writeJSON(w, http.StatusOK, result)
}

func sameServiceCheckResources(before []servicecenter.Resource, after []servicecenter.ResourceProjection) bool {
	if len(before) != len(after) {
		return false
	}
	identities := make(map[string]servicecenter.Resource, len(before))
	for _, resource := range before {
		identities[resource.ID] = resource
	}
	for _, resource := range after {
		previous, ok := identities[resource.ID]
		if !ok || previous.ResourceType != resource.ResourceType || previous.ScopeID != resource.ScopeID || previous.ResourceKind != resource.ResourceKind || previous.Namespace != resource.Namespace || previous.ResourceKey != resource.ResourceKey {
			return false
		}
	}
	return true
}
