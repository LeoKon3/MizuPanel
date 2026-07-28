package servicecenter

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/mizupanel/mizupanel/internal/protocol"
)

type nodeSignal struct {
	Name   string
	Status string
}

type uptimeSignal struct {
	Name    string
	Target  string
	Enabled bool
	Status  string
}

type alertSignal struct {
	Name        string
	Enabled     bool
	ActiveCount int
}

type taskSignal struct {
	Name         string
	Enabled      bool
	LatestStatus string
	HasRun       bool
}

func nodeHealth(resource Resource, signal nodeSignal, exists bool) ResourceProjection {
	if !exists {
		return projection(resource, HealthUnknown, "missing", "关联节点已不存在")
	}
	if strings.EqualFold(signal.Status, "online") {
		return projection(resource, HealthHealthy, "available", "")
	}
	return projection(resource, HealthUnhealthy, "available", "节点离线")
}

func uptimeHealth(resource Resource, signal uptimeSignal, exists bool) ResourceProjection {
	if !exists {
		return projection(resource, HealthUnknown, "missing", "关联拨测已不存在")
	}
	if !signal.Enabled {
		return projection(resource, HealthDegraded, "available", "服务拨测已禁用")
	}
	switch strings.ToLower(signal.Status) {
	case "up":
		return projection(resource, HealthHealthy, "available", "")
	case "warning":
		return projection(resource, HealthDegraded, "available", "服务拨测处于警告状态")
	case "down":
		return projection(resource, HealthUnhealthy, "available", "服务拨测失败")
	default:
		return projection(resource, HealthUnknown, "available", "服务拨测尚无可用结果")
	}
}

func alertHealth(resource Resource, signal alertSignal, exists bool) ResourceProjection {
	if !exists {
		return projection(resource, HealthUnknown, "missing", "关联告警规则已不存在")
	}
	if !signal.Enabled {
		return projection(resource, HealthDegraded, "available", "告警规则已禁用")
	}
	if signal.ActiveCount > 0 {
		return projection(resource, HealthUnhealthy, "available", fmt.Sprintf("存在 %d 条未恢复告警", signal.ActiveCount))
	}
	return projection(resource, HealthHealthy, "available", "")
}

func taskHealth(resource Resource, signal taskSignal, exists bool) ResourceProjection {
	if !exists {
		return projection(resource, HealthUnknown, "missing", "关联计划任务已不存在")
	}
	if !signal.Enabled {
		return projection(resource, HealthDegraded, "available", "计划任务已禁用")
	}
	if !signal.HasRun {
		return projection(resource, HealthUnknown, "available", "计划任务尚无执行记录")
	}
	switch strings.ToLower(signal.LatestStatus) {
	case "queued", "running", "success":
		return projection(resource, HealthHealthy, "available", "")
	case "partial", "failed", "skipped", "interrupted":
		return projection(resource, HealthDegraded, "available", "计划任务最近执行状态为 "+signal.LatestStatus)
	default:
		return projection(resource, HealthUnknown, "available", "计划任务最近执行状态暂不可判定")
	}
}

func composeHealth(resource Resource, project *protocol.DockerComposeProject, unavailable string) ResourceProjection {
	if unavailable != "" {
		return projection(resource, HealthDegraded, "unavailable", "Compose 状态暂不可用")
	}
	if project == nil {
		return projection(resource, HealthUnknown, "missing", "关联 Compose 项目已不存在")
	}
	if project.Error != "" {
		return projection(resource, HealthDegraded, "unavailable", "Compose 项目状态读取不完整")
	}
	status := strings.ToLower(strings.TrimSpace(project.Status))
	if status == "stopped" || status == "exited" || status == "dead" {
		return projection(resource, HealthUnhealthy, "available", "Compose 项目已停止")
	}
	if len(project.Services) == 0 {
		return projection(resource, HealthUnhealthy, "available", "Compose 项目没有运行中的服务")
	}
	degraded := false
	for _, service := range project.Services {
		state := strings.ToLower(strings.TrimSpace(service.State))
		health := strings.ToLower(strings.TrimSpace(service.Health))
		if state == "exited" || state == "dead" || health == "unhealthy" {
			return projection(resource, HealthUnhealthy, "available", "Compose 服务 "+service.Name+" 状态异常")
		}
		if state != "running" || health == "starting" {
			degraded = true
		}
	}
	if degraded {
		return projection(resource, HealthDegraded, "available", "部分 Compose 服务状态暂未稳定")
	}
	return projection(resource, HealthHealthy, "available", "")
}

func systemdHealth(resource Resource, service *protocol.SystemdService, unavailable string) ResourceProjection {
	if unavailable != "" {
		return projection(resource, HealthDegraded, "unavailable", "Systemd 状态暂不可用")
	}
	if service == nil {
		return projection(resource, HealthUnknown, "missing", "关联 Systemd 服务已不存在")
	}
	active := strings.ToLower(service.ActiveState)
	sub := strings.ToLower(service.SubState)
	if active == "active" && sub == "running" {
		return projection(resource, HealthHealthy, "available", "")
	}
	if active == "activating" || active == "reloading" {
		return projection(resource, HealthDegraded, "available", "Systemd 服务正在"+service.ActiveState)
	}
	if active == "failed" || active == "inactive" || active == "deactivating" {
		return projection(resource, HealthUnhealthy, "available", "Systemd 服务状态为 "+service.ActiveState)
	}
	return projection(resource, HealthUnknown, "available", "Systemd 服务状态暂不可判定")
}

func k8sReadyHealth(resource Resource, ready string, available *int32, unavailable string) ResourceProjection {
	if unavailable != "" {
		return projection(resource, HealthDegraded, "unavailable", "Kubernetes 工作负载状态暂不可用")
	}
	if ready == "" {
		return projection(resource, HealthUnknown, "missing", "关联 Kubernetes 工作负载已不存在")
	}
	current, desired, ok := parseReady(ready)
	if !ok || desired <= 0 {
		return projection(resource, HealthUnknown, "available", "Kubernetes 副本状态暂不可判定")
	}
	if current == 0 {
		return projection(resource, HealthUnhealthy, "available", fmt.Sprintf("Kubernetes 工作负载无就绪副本（%d/%d）", current, desired))
	}
	if current < desired || (available != nil && int(*available) < desired) {
		return projection(resource, HealthDegraded, "available", fmt.Sprintf("Kubernetes 工作负载部分副本未就绪（%d/%d）", current, desired))
	}
	return projection(resource, HealthHealthy, "available", "")
}

func k8sDaemonHealth(resource Resource, workload *protocol.K8sDaemonSet, unavailable string) ResourceProjection {
	if unavailable != "" {
		return projection(resource, HealthDegraded, "unavailable", "Kubernetes 工作负载状态暂不可用")
	}
	if workload == nil {
		return projection(resource, HealthUnknown, "missing", "关联 Kubernetes 工作负载已不存在")
	}
	desired, ready := int(workload.Desired), int(workload.Ready)
	if desired <= 0 {
		return projection(resource, HealthUnknown, "available", "Kubernetes 副本状态暂不可判定")
	}
	if ready == 0 {
		return projection(resource, HealthUnhealthy, "available", fmt.Sprintf("Kubernetes 工作负载无就绪副本（%d/%d）", ready, desired))
	}
	if ready < desired || int(workload.Available) < desired {
		return projection(resource, HealthDegraded, "available", fmt.Sprintf("Kubernetes 工作负载部分副本未就绪（%d/%d）", ready, desired))
	}
	return projection(resource, HealthHealthy, "available", "")
}

func parseReady(value string) (int, int, bool) {
	parts := strings.Split(strings.TrimSpace(value), "/")
	if len(parts) != 2 {
		return 0, 0, false
	}
	current, errCurrent := strconv.Atoi(strings.TrimSpace(parts[0]))
	desired, errDesired := strconv.Atoi(strings.TrimSpace(parts[1]))
	if errCurrent != nil || errDesired != nil || current < 0 || desired < 0 {
		return 0, 0, false
	}
	return current, desired, true
}

func projection(resource Resource, health HealthStatus, state, reason string) ResourceProjection {
	return ResourceProjection{Resource: resource, Health: health, State: state, Reason: reason, Meta: map[string]any{}}
}

func aggregateHealth(resources []ResourceProjection) (HealthStatus, []HealthReason, map[string]int) {
	reasons := make([]HealthReason, 0)
	counts := map[string]int{"unhealthy": 0, "degraded": 0, "unknown": 0}
	healthyCount := 0
	for _, resource := range resources {
		if resource.Health == HealthHealthy {
			healthyCount++
			continue
		}
		counts[string(resource.Health)]++
		reasons = append(reasons, HealthReason{Status: resource.Health, ResourceID: resource.ID, ResourceType: resource.ResourceType, ResourceName: resource.DisplayName, Message: resource.Reason})
	}
	sort.SliceStable(reasons, func(i, j int) bool {
		left, right := healthPriority(reasons[i].Status), healthPriority(reasons[j].Status)
		if left != right {
			return left < right
		}
		if reasons[i].ResourceName != reasons[j].ResourceName {
			return reasons[i].ResourceName < reasons[j].ResourceName
		}
		return reasons[i].ResourceID < reasons[j].ResourceID
	})
	switch {
	case counts[string(HealthUnhealthy)] > 0:
		return HealthUnhealthy, reasons, counts
	case counts[string(HealthDegraded)] > 0:
		return HealthDegraded, reasons, counts
	case counts[string(HealthUnknown)] > 0 && healthyCount > 0:
		return HealthDegraded, reasons, counts
	case healthyCount > 0:
		return HealthHealthy, reasons, counts
	default:
		return HealthUnknown, reasons, counts
	}
}

func healthPriority(status HealthStatus) int {
	switch status {
	case HealthUnhealthy:
		return 0
	case HealthDegraded:
		return 1
	case HealthUnknown:
		return 2
	default:
		return 3
	}
}
