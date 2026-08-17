package ai

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/mizupanel/mizupanel/internal/server/store"
)

var ErrAIControlPaused = errors.New("AI control plane is paused")

type PolicyDecision string

const (
	PolicyRead              PolicyDecision = "read"
	PolicyManual            PolicyDecision = "manual_confirmation"
	PolicyAutonomous        PolicyDecision = "autonomous_allowed"
	PolicyBlockedPaused     PolicyDecision = "blocked_paused"
	PolicyBlockedScope      PolicyDecision = "blocked_scope"
	PolicyBlockedAction     PolicyDecision = "blocked_action"
	PolicyBlockedCapability PolicyDecision = "blocked_capability"
	PolicyBlockedVerifier   PolicyDecision = "blocked_verifier"
	PolicyBlockedPolicy     PolicyDecision = "blocked_policy"
)

type PolicyResult struct {
	Decision PolicyDecision `json:"decision"`
	Reason   string         `json:"reason"`
	Revision int64          `json:"revision"`
	Action   string         `json:"action,omitempty"`
}

type PolicyStore interface {
	AIControlPolicy(context.Context) (store.AIControlPolicy, error)
}

type PolicyMetadata struct {
	ResourceDomain string
	Action         string
	AutonomyClass  string
	TargetScope    string
	RequiresNode   bool
	Autonomous     bool
	Verifier       string
}

func classifiedPolicyMetadata(domain, action, class, targetScope string) PolicyMetadata {
	return PolicyMetadata{
		ResourceDomain: domain,
		Action:         action,
		AutonomyClass:  class,
		TargetScope:    targetScope,
		RequiresNode:   targetScope == "node" || targetScope == "multi_node" || targetScope == "node_resource",
	}
}

func autonomousPolicyMetadata(domain, action, targetScope, verifier string) PolicyMetadata {
	metadata := classifiedPolicyMetadata(domain, action, "low", targetScope)
	metadata.Autonomous = true
	metadata.Verifier = verifier
	return metadata
}

func (p PolicyResult) SafeReason() string {
	if p.Reason == "" {
		return "policy_denied"
	}
	return p.Reason
}

func evaluatePolicy(policy store.AIControlPolicy, call ValidatedToolCall) PolicyResult {
	result := PolicyResult{Decision: PolicyManual, Reason: "confirmation_required", Revision: policy.Revision, Action: call.ActionKey}
	if call.Risk == RiskRead {
		result.Decision, result.Reason = PolicyRead, "read_only"
		return result
	}
	if policy.Mode == store.AIControlPaused {
		result.Decision, result.Reason = PolicyBlockedPaused, "paused"
		return result
	}
	if policy.Mode != store.AIControlLowRiskAuto || !call.Metadata.Autonomous {
		return result
	}
	if call.Metadata.Verifier == "" {
		result.Decision, result.Reason = PolicyBlockedVerifier, "verifier_unavailable"
		return result
	}
	if call.Target.NodeID == "" || !contains(policy.NodeScope, call.Target.NodeID) {
		result.Decision, result.Reason = PolicyBlockedScope, "node_outside_scope"
		return result
	}
	if !contains(policy.AllowedActions, call.ActionKey) {
		result.Decision, result.Reason = PolicyBlockedAction, "action_not_allowed"
		return result
	}
	result.Decision, result.Reason = PolicyAutonomous, "low_risk_policy"
	return result
}

func contains(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func isAutonomousRecoveryAction(action string) bool {
	switch action {
	case store.AIControlActionDockerContainerStart, store.AIControlActionDockerContainerRestart,
		store.AIControlActionComposeServiceStart, store.AIControlActionComposeServiceRestart,
		store.AIControlActionSystemdServiceStart, store.AIControlActionSystemdServiceRestart:
		return true
	default:
		return false
	}
}

func (r *Registry) ControlPolicy(ctx context.Context) (store.AIControlPolicy, error) {
	policy := store.DefaultAIControlPolicy()
	if r != nil && r.dependencies.Settings != nil {
		loaded, err := r.dependencies.Settings.AIControlPolicy(ctx)
		if err != nil {
			return store.AIControlPolicy{}, err
		}
		policy = loaded
	}
	return policy, nil
}

func policyAction(raw json.RawMessage, prefix string, composeUp bool) string {
	var args struct {
		Action string `json:"action"`
	}
	if json.Unmarshal(raw, &args) != nil {
		return ""
	}
	action := strings.TrimSpace(args.Action)
	if composeUp && action == "up" {
		action = "start"
	}
	return prefix + "." + action
}

func (r *Registry) Policy(ctx context.Context, call ValidatedToolCall) (PolicyResult, error) {
	if call.Risk == RiskRead {
		return evaluatePolicy(store.DefaultAIControlPolicy(), call), nil
	}
	policy, err := r.ControlPolicy(ctx)
	if err != nil {
		return PolicyResult{}, err
	}
	result := evaluatePolicy(policy, call)
	if result.Decision == PolicyAutonomous {
		tool, ok := r.tools[call.Definition.Name]
		if !ok || tool.verify == nil || tool.metadata.Verifier == "" {
			result.Decision, result.Reason = PolicyBlockedVerifier, "verifier_unavailable"
		}
	}
	return result, nil
}

func (r *Registry) PolicyForStoredCall(ctx context.Context, step store.AIToolCall) (PolicyResult, error) {
	arguments := json.RawMessage(step.ArgumentsJSON)
	if strings.HasPrefix(step.ArgumentsJSON, `{"sealed_tool_arguments":`) {
		return PolicyResult{Decision: PolicyBlockedCapability, Reason: "sealed_arguments_revalidation_required"}, nil
	}
	call, err := r.Validate(ctx, step.ToolName, arguments)
	if err != nil {
		return PolicyResult{Decision: PolicyBlockedCapability, Reason: "capability_offline"}, nil
	}
	return r.Policy(ctx, call)
}

func policyError(result PolicyResult) error {
	switch result.Decision {
	case PolicyBlockedPaused:
		return ErrAIControlPaused
	case PolicyBlockedScope:
		return errors.New("AI target is outside the autonomous node scope")
	case PolicyBlockedAction:
		return errors.New("AI action is not allowed by policy")
	case PolicyBlockedVerifier:
		return errors.New("AI final-state verification is unavailable")
	case PolicyBlockedCapability:
		return errors.New("AI target capability is unavailable")
	case PolicyBlockedPolicy:
		return errors.New("AI control policy is unavailable")
	default:
		return nil
	}
}
