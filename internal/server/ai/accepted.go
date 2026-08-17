package ai

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/mizupanel/mizupanel/internal/protocol"
	"github.com/mizupanel/mizupanel/internal/server/store"
	"github.com/mizupanel/mizupanel/internal/version"
)

const (
	acceptedOperationPollInterval = 2 * time.Second
	acceptedOperationMaxAge       = 31 * time.Minute
	acceptedRebootGracePeriod     = 2 * time.Second
)

type rebootEvidenceProvider interface {
	RebootCompletedAfter(context.Context, string, time.Time) (bool, error)
}

type agentUpgradeStatusProvider interface {
	AgentUpgradeStatus(string) protocol.AgentUpgradeStatus
}

// RunAcceptedOperationVerifier resumes accepted operations after startup and
// keeps the persisted ToolCall status in sync without replaying the operation.
func (s *Service) RunAcceptedOperationVerifier(ctx context.Context) {
	if s == nil || s.store == nil {
		return
	}
	_ = s.verifyAcceptedOperations(ctx)
	ticker := time.NewTicker(acceptedOperationPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = s.verifyAcceptedOperations(ctx)
		}
	}
}

func (s *Service) verifyAcceptedOperations(ctx context.Context) error {
	calls, err := s.store.ListAcceptedToolCalls(ctx, 100)
	if err != nil {
		return err
	}
	var firstErr error
	for _, call := range calls {
		if err := s.verifyAcceptedOperation(ctx, call); err != nil {
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

func (s *Service) verifyAcceptedOperation(ctx context.Context, call store.AIToolCall) error {
	now := s.now().UTC()
	if !call.UpdatedAt.IsZero() && now.Sub(call.UpdatedAt) >= acceptedOperationMaxAge {
		return s.completeAcceptedOperation(ctx, call.ID, "failure", "操作结果在限定时间内未确认", "操作结果在限定时间内未确认，未重试。")
	}

	switch call.ToolName {
	case "reboot_node":
		evidence, ok := s.registry.dependencies.AgentOps.(rebootEvidenceProvider)
		if !ok || call.NodeID == "" {
			return s.completeAcceptedOperation(ctx, call.ID, "interrupted", "服务无法确认重启结果，未重试", "服务无法确认重启结果，未重试。")
		}
		if now.Sub(call.UpdatedAt) < acceptedRebootGracePeriod {
			return nil
		}
		completed, err := evidence.RebootCompletedAfter(ctx, call.NodeID, call.UpdatedAt)
		if err != nil {
			return err
		}
		if !completed {
			return nil
		}
		return s.completeAcceptedOperation(ctx, call.ID, "success", "节点已重新上线", "节点已重新上线，重启操作完成。")

	case "upgrade_agent":
		statusProvider, ok := s.registry.dependencies.AgentOps.(agentUpgradeStatusProvider)
		if !ok || call.NodeID == "" {
			return s.completeAcceptedOperation(ctx, call.ID, "interrupted", "服务无法确认 Agent 升级结果，未重试", "服务无法确认 Agent 升级结果，未重试。")
		}
		status := statusProvider.AgentUpgradeStatus(call.NodeID)
		switch status.Stage {
		case "completed":
			return s.completeAcceptedOperation(ctx, call.ID, "success", "Agent 升级完成", "Agent 升级完成。")
		case "failed":
			return s.completeAcceptedOperation(ctx, call.ID, "failure", "Agent 升级失败", "Agent 升级失败，未重试。")
		case "idle":
			// Upgrade state is kept in memory by AgentHub. After a Server restart,
			// an already upgraded Agent reconnects with an idle status instead of
			// restoring the previous in-memory completed record.
			if status.ActualVersion == version.Current {
				return s.completeAcceptedOperation(ctx, call.ID, "success", "Agent 升级完成", "Agent 升级完成。")
			}
			return nil
		default:
			return nil
		}

	case "run_saved_script":
		runID, err := strconv.ParseInt(call.OperationID, 10, 64)
		if err != nil || runID <= 0 || s.registry.dependencies.Tasks == nil {
			return s.completeAcceptedOperation(ctx, call.ID, "interrupted", "服务重启后无法确认脚本运行结果，未重试", "服务重启后无法确认脚本运行结果，未重试。")
		}
		run, err := s.registry.dependencies.Tasks.GetRun(ctx, runID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return s.completeAcceptedOperation(ctx, call.ID, "failure", "脚本运行记录不存在", "脚本运行记录不存在，未重试。")
			}
			return err
		}
		switch run.Status {
		case store.RunStatusSuccess:
			return s.completeAcceptedOperation(ctx, call.ID, "success", "脚本任务执行成功", "脚本任务执行成功。")
		case store.RunStatusPartial, store.RunStatusFailed, store.RunStatusSkipped, store.RunStatusInterrupted:
			return s.completeAcceptedOperation(ctx, call.ID, "failure", "脚本任务执行未完全成功", "脚本任务执行未完全成功，未重试。")
		default:
			return nil
		}
	default:
		return s.completeAcceptedOperation(ctx, call.ID, "interrupted", "未知异步操作未执行重试", "未知异步操作无法确认，未重试。")
	}
}

func (s *Service) completeAcceptedOperation(ctx context.Context, id, status, summary, assistant string) error {
	call, turn, err := s.store.GetToolCall(ctx, id)
	if err != nil {
		if err == store.ErrAIConflict || err == store.ErrAINotFound {
			return nil
		}
		return err
	}
	if call.StepIndex >= 0 {
		verificationStatus := ""
		if status == "success" {
			if tool, ok := s.registry.tools[call.ToolName]; ok && tool.metadata.Verifier != "" {
				validated, validateErr := s.revalidatePlanStep(ctx, call)
				if validateErr != nil {
					status, summary, verificationStatus = "interrupted", "最终状态无法确认，未自动重试", string(VerificationUnknown)
				} else {
					verification := s.registry.Verify(ctx, validated, SafeToolResult{Status: "success"}, call.UpdatedAt)
					verificationStatus, summary = string(verification.Status), verification.Summary
					switch verification.Status {
					case VerificationFailure:
						status = "failure"
					case VerificationUnknown:
						status = "interrupted"
					}
				}
			}
		}
		steps, listErr := s.store.ListPlanSteps(ctx, turn.ID)
		if listErr != nil {
			return listErr
		}
		if status == "success" && call.StepIndex < len(steps)-1 {
			if err := s.store.TransitionToolPlanStepWithVerification(ctx, id, "accepted", status, summary, "", verificationStatus); err != nil {
				if err == store.ErrAIConflict || err == store.ErrAINotFound {
					return nil
				}
				return err
			}
			autonomous := call.PolicyDecision == string(PolicyAutonomous)
			_, err = s.advancePlan(ctx, turn, nil, autonomous)
		} else {
			_, err = s.completePlanStepVerified(ctx, turn.ID, id, "accepted", status, summary, "", verificationStatus)
		}
		if err == store.ErrAIConflict || err == store.ErrAINotFound {
			return nil
		}
		return err
	}
	_, _, _, err = s.store.CompleteAcceptedToolCall(ctx, id, status, summary, assistant)
	if err == store.ErrAIConflict || err == store.ErrAINotFound {
		return nil
	}
	return err
}
