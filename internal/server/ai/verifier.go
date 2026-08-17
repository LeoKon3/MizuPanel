package ai

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

type VerificationStatus string

const (
	VerificationSuccess VerificationStatus = "success"
	VerificationFailure VerificationStatus = "failure"
	VerificationUnknown VerificationStatus = "unknown"
)

type VerificationResult struct {
	Status  VerificationStatus `json:"status"`
	Summary string             `json:"summary"`
}

func (r *Registry) Verify(ctx context.Context, call ValidatedToolCall, result SafeToolResult, notBefore time.Time) VerificationResult {
	tool, ok := r.tools[call.Definition.Name]
	if !ok {
		return VerificationResult{Status: VerificationUnknown, Summary: "验证器不可用"}
	}
	if tool.metadata.Verifier == "" {
		return VerificationResult{Status: VerificationSuccess, Summary: "操作结果已确认"}
	}
	if tool.verify == nil {
		return VerificationResult{Status: VerificationUnknown, Summary: "验证器不可用"}
	}
	if ctx.Err() != nil {
		return VerificationResult{Status: VerificationUnknown, Summary: "验证已取消"}
	}
	return tool.verify(ctx, call.Arguments, result, notBefore)
}

func verificationContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, 8*time.Second)
}

func (r *Registry) verifyDockerRunning(ctx context.Context, raw json.RawMessage, result SafeToolResult, notBefore time.Time) VerificationResult {
	if result.Status == "failure" || result.Status == "unsupported" {
		return VerificationResult{Status: VerificationFailure, Summary: "Docker 操作失败"}
	}
	var args struct {
		NodeID      string `json:"node_id"`
		ContainerID string `json:"container_id"`
	}
	if json.Unmarshal(raw, &args) != nil || r.dependencies.Docker == nil {
		return VerificationResult{Status: VerificationUnknown, Summary: "Docker 状态未知"}
	}
	verifyCtx, cancel := verificationContext(ctx)
	defer cancel()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		snapshot, found, err := r.dependencies.Docker.Get(verifyCtx, args.NodeID)
		fresh := notBefore.IsZero() || snapshot.CollectedAt > notBefore.Unix()
		if err == nil && found && fresh {
			if !snapshot.Available {
				return VerificationResult{Status: VerificationUnknown, Summary: "Docker 状态未知"}
			}
			for _, container := range snapshot.Containers {
				if container.ID == args.ContainerID || container.FullID == args.ContainerID || strings.TrimPrefix(container.Name, "/") == args.ContainerID {
					if strings.EqualFold(container.State, "running") {
						return VerificationResult{Status: VerificationSuccess, Summary: "Docker 已确认运行"}
					}
					return VerificationResult{Status: VerificationFailure, Summary: "Docker 未进入运行状态"}
				}
			}
			return VerificationResult{Status: VerificationUnknown, Summary: "未找到 Docker 容器状态"}
		}
		select {
		case <-verifyCtx.Done():
			return VerificationResult{Status: VerificationUnknown, Summary: "Docker 状态验证超时"}
		case <-ticker.C:
		}
	}
}

func (r *Registry) verifyComposeRunning(ctx context.Context, raw json.RawMessage, result SafeToolResult, _ time.Time) VerificationResult {
	if result.Status == "failure" || result.Status == "unsupported" {
		return VerificationResult{Status: VerificationFailure, Summary: "Compose 操作失败"}
	}
	var args struct {
		NodeID      string `json:"node_id"`
		ProjectName string `json:"project_name"`
		ServiceName string `json:"service_name"`
	}
	if json.Unmarshal(raw, &args) != nil || r.dependencies.AgentOps == nil {
		return VerificationResult{Status: VerificationUnknown, Summary: "Compose 状态未知"}
	}
	verifyCtx, cancel := verificationContext(ctx)
	defer cancel()
	response, err := r.dependencies.AgentOps.DockerComposeList(verifyCtx, args.NodeID)
	if err != nil || !response.Success || !response.Supported {
		return VerificationResult{Status: VerificationUnknown, Summary: "Compose 状态未知"}
	}
	for _, project := range response.Projects {
		if project.Name != args.ProjectName {
			continue
		}
		if args.ServiceName == "" {
			if strings.Contains(strings.ToLower(project.Status), "running") || strings.Contains(strings.ToLower(project.Status), "up") {
				return VerificationResult{Status: VerificationSuccess, Summary: "Compose 项目已确认运行"}
			}
			return VerificationResult{Status: VerificationFailure, Summary: "Compose 项目未进入运行状态"}
		}
		for _, service := range project.Services {
			if service.Name == args.ServiceName {
				if strings.EqualFold(service.State, "running") || strings.Contains(strings.ToLower(service.Status), "up") {
					return VerificationResult{Status: VerificationSuccess, Summary: "Compose 服务已确认运行"}
				}
				return VerificationResult{Status: VerificationFailure, Summary: "Compose 服务未进入运行状态"}
			}
		}
		return VerificationResult{Status: VerificationUnknown, Summary: "未找到 Compose 服务状态"}
	}
	return VerificationResult{Status: VerificationUnknown, Summary: "未找到 Compose 项目状态"}
}

func (r *Registry) verifySystemdRunning(ctx context.Context, raw json.RawMessage, result SafeToolResult, _ time.Time) VerificationResult {
	if result.Status == "failure" || result.Status == "unsupported" {
		return VerificationResult{Status: VerificationFailure, Summary: "Systemd 操作失败"}
	}
	var args struct {
		NodeID      string `json:"node_id"`
		ServiceName string `json:"service_name"`
	}
	if json.Unmarshal(raw, &args) != nil || r.dependencies.AgentOps == nil {
		return VerificationResult{Status: VerificationUnknown, Summary: "Systemd 状态未知"}
	}
	verifyCtx, cancel := verificationContext(ctx)
	defer cancel()
	response, err := r.dependencies.AgentOps.SystemdServiceList(verifyCtx, args.NodeID)
	if err != nil || !response.Success || !response.Supported {
		return VerificationResult{Status: VerificationUnknown, Summary: "Systemd 状态未知"}
	}
	for _, service := range response.Services {
		if service.Name == args.ServiceName {
			if service.ActiveState == "active" {
				return VerificationResult{Status: VerificationSuccess, Summary: "Systemd 服务已确认运行"}
			}
			return VerificationResult{Status: VerificationFailure, Summary: "Systemd 服务未进入运行状态"}
		}
	}
	return VerificationResult{Status: VerificationUnknown, Summary: "未找到 Systemd 服务状态"}
}
