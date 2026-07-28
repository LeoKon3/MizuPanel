package servicecenter

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

var (
	ErrInvalid  = errors.New("invalid application service")
	ErrNotFound = errors.New("application service not found")
	ErrConflict = errors.New("application service conflict")
)

type ResourceType string

const (
	ResourceNode           ResourceType = "node"
	ResourceComposeProject ResourceType = "compose_project"
	ResourceSystemdService ResourceType = "systemd_service"
	ResourceK8sWorkload    ResourceType = "k8s_workload"
	ResourceUptimeMonitor  ResourceType = "uptime_monitor"
	ResourceAlertRule      ResourceType = "alert_rule"
	ResourceScheduledTask  ResourceType = "scheduled_task"
)

type HealthStatus string

const (
	HealthHealthy   HealthStatus = "healthy"
	HealthDegraded  HealthStatus = "degraded"
	HealthUnhealthy HealthStatus = "unhealthy"
	HealthUnknown   HealthStatus = "unknown"
)

type Service struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Resources   []Resource `json:"resources"`
	CreatedAt   string     `json:"created_at"`
	UpdatedAt   string     `json:"updated_at"`
}

type Resource struct {
	ID           string       `json:"id"`
	ServiceID    string       `json:"service_id,omitempty"`
	ResourceType ResourceType `json:"resource_type"`
	ScopeID      string       `json:"scope_id"`
	ResourceKind string       `json:"resource_kind"`
	Namespace    string       `json:"namespace"`
	ResourceKey  string       `json:"resource_key"`
	DisplayName  string       `json:"display_name"`
	CreatedAt    string       `json:"created_at,omitempty"`
}

type ServiceInput struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Resources   []Resource `json:"resources"`
}

type HealthReason struct {
	Status       HealthStatus `json:"status"`
	ResourceID   string       `json:"resource_id"`
	ResourceType ResourceType `json:"resource_type"`
	ResourceName string       `json:"resource_name"`
	Message      string       `json:"message"`
}

type ResourceProjection struct {
	Resource
	Health HealthStatus   `json:"health"`
	State  string         `json:"state"`
	Reason string         `json:"reason"`
	Meta   map[string]any `json:"meta"`
}

type ServiceSummary struct {
	ID                 string               `json:"id"`
	Name               string               `json:"name"`
	Description        string               `json:"description"`
	Health             HealthStatus         `json:"health"`
	Reasons            []HealthReason       `json:"reasons"`
	FirstReason        string               `json:"first_reason"`
	ReasonCounts       map[string]int       `json:"reason_counts"`
	ResourceCount      int                  `json:"resource_count"`
	ResourceTypeCounts map[string]int       `json:"resource_type_counts"`
	LocationSummary    string               `json:"location_summary"`
	Resources          []ResourceProjection `json:"resources"`
	CreatedAt          string               `json:"created_at"`
	UpdatedAt          string               `json:"updated_at"`
}

type AlertActivity struct {
	ID          int64   `json:"id"`
	RuleID      int64   `json:"rule_id"`
	RuleName    string  `json:"rule_name"`
	NodeID      string  `json:"node_id"`
	NodeName    string  `json:"node_name"`
	MetricField string  `json:"metric_field"`
	MetricValue float64 `json:"metric_value"`
	TriggeredAt string  `json:"triggered_at"`
	ResolvedAt  *string `json:"resolved_at"`
}

type TaskActivity struct {
	ID          int64   `json:"id"`
	TaskID      *int64  `json:"task_id"`
	TaskName    string  `json:"task_name"`
	ScriptName  string  `json:"script_name"`
	Status      string  `json:"status"`
	Trigger     string  `json:"trigger"`
	CreatedAt   string  `json:"created_at"`
	CompletedAt *string `json:"completed_at"`
}

type AuditActivity struct {
	ID         int64             `json:"id"`
	CreatedAt  string            `json:"created_at"`
	ActorType  string            `json:"actor_type"`
	ActorName  string            `json:"actor_name"`
	Module     string            `json:"module"`
	Action     string            `json:"action"`
	TargetType string            `json:"target_type"`
	TargetID   string            `json:"target_id"`
	TargetName string            `json:"target_name"`
	NodeID     string            `json:"node_id"`
	Result     string            `json:"result"`
	Summary    string            `json:"summary"`
	Metadata   map[string]string `json:"metadata"`
}

type ServiceDetail struct {
	ServiceSummary
	RecentAlerts []AlertActivity `json:"recent_alerts"`
	RecentTasks  []TaskActivity  `json:"recent_tasks"`
	RecentAudit  []AuditActivity `json:"recent_audit"`
}

const maxResourcesPerService = 200

func normalizeInput(input ServiceInput) (ServiceInput, string, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	if input.Resources == nil {
		input.Resources = []Resource{}
	}
	if input.Name == "" || !utf8.ValidString(input.Name) || utf8.RuneCountInString(input.Name) > 128 {
		return ServiceInput{}, "", fmt.Errorf("%w: 服务名称不能为空且不能超过 128 个字符", ErrInvalid)
	}
	if !utf8.ValidString(input.Description) || len(input.Description) > 2048 {
		return ServiceInput{}, "", fmt.Errorf("%w: 服务描述不能超过 2048 字节", ErrInvalid)
	}
	if len(input.Resources) > maxResourcesPerService {
		return ServiceInput{}, "", fmt.Errorf("%w: 单个服务最多关联 %d 个资源", ErrInvalid, maxResourcesPerService)
	}
	seen := make(map[string]struct{}, len(input.Resources))
	for i := range input.Resources {
		resource, err := normalizeResource(input.Resources[i])
		if err != nil {
			return ServiceInput{}, "", err
		}
		identity := resourceIdentity(resource)
		if _, exists := seen[identity]; exists {
			return ServiceInput{}, "", fmt.Errorf("%w: 同一资源不能重复关联", ErrConflict)
		}
		seen[identity] = struct{}{}
		input.Resources[i] = resource
	}
	return input, strings.ToLower(input.Name), nil
}

func normalizeResource(resource Resource) (Resource, error) {
	resource.ID = ""
	resource.ServiceID = ""
	resource.CreatedAt = ""
	resource.ScopeID = strings.TrimSpace(resource.ScopeID)
	resource.ResourceKind = strings.ToLower(strings.TrimSpace(resource.ResourceKind))
	resource.Namespace = strings.TrimSpace(resource.Namespace)
	resource.ResourceKey = strings.TrimSpace(resource.ResourceKey)
	resource.DisplayName = strings.TrimSpace(resource.DisplayName)
	if !validBounded(resource.ScopeID, 191) || !validBounded(resource.ResourceKind, 32) || !validBounded(resource.Namespace, 191) || !validBounded(resource.ResourceKey, 255) || resource.ResourceKey == "" || !validBounded(resource.DisplayName, 256) {
		return Resource{}, fmt.Errorf("%w: 资源标识无效或过长", ErrInvalid)
	}
	if resource.DisplayName == "" {
		resource.DisplayName = resource.ResourceKey
	}
	switch resource.ResourceType {
	case ResourceNode:
		if resource.ScopeID != "" || resource.ResourceKind != "" || resource.Namespace != "" {
			return Resource{}, fmt.Errorf("%w: 节点资源身份格式无效", ErrInvalid)
		}
	case ResourceComposeProject:
		if resource.ScopeID == "" || resource.Namespace != "" || (resource.ResourceKind != "managed" && resource.ResourceKind != "external") {
			return Resource{}, fmt.Errorf("%w: Compose 项目身份格式无效", ErrInvalid)
		}
		if resource.ResourceKind == "managed" {
			parsed, err := uuid.Parse(resource.ResourceKey)
			if err != nil || parsed == uuid.Nil {
				return Resource{}, fmt.Errorf("%w: 托管 Compose 项目 ID 无效", ErrInvalid)
			}
			resource.ResourceKey = parsed.String()
		}
	case ResourceSystemdService:
		if resource.ScopeID == "" || resource.ResourceKind != "" || resource.Namespace != "" {
			return Resource{}, fmt.Errorf("%w: Systemd 服务身份格式无效", ErrInvalid)
		}
	case ResourceK8sWorkload:
		if resource.ScopeID == "" || resource.Namespace == "" || !validK8sKind(resource.ResourceKind) {
			return Resource{}, fmt.Errorf("%w: Kubernetes 工作负载身份格式无效", ErrInvalid)
		}
	case ResourceUptimeMonitor, ResourceAlertRule, ResourceScheduledTask:
		if resource.ScopeID != "" || resource.ResourceKind != "" || resource.Namespace != "" {
			return Resource{}, fmt.Errorf("%w: 数据库资源身份格式无效", ErrInvalid)
		}
		id, err := strconv.ParseInt(resource.ResourceKey, 10, 64)
		if err != nil || id <= 0 {
			return Resource{}, fmt.Errorf("%w: 资源 ID 必须为正整数", ErrInvalid)
		}
		resource.ResourceKey = strconv.FormatInt(id, 10)
	default:
		return Resource{}, fmt.Errorf("%w: 不支持的资源类型 %q", ErrInvalid, resource.ResourceType)
	}
	return resource, nil
}

func validBounded(value string, maxBytes int) bool {
	return utf8.ValidString(value) && len(value) <= maxBytes
}

func validK8sKind(kind string) bool {
	return kind == "deployment" || kind == "statefulset" || kind == "daemonset"
}

func resourceIdentity(resource Resource) string {
	return strings.Join([]string{string(resource.ResourceType), resource.ScopeID, resource.ResourceKind, resource.Namespace, resource.ResourceKey}, "\x00")
}
