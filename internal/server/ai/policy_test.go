package ai

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/mizupanel/mizupanel/internal/server/store"
)

func TestEvaluatePolicyDefaultsAndScopeFailClosed(t *testing.T) {
	call := ValidatedToolCall{
		Definition: ToolDefinition{Name: "docker_container_action"},
		Risk:       RiskConfirm,
		Target:     ToolTarget{NodeID: "node-1"},
		Metadata: PolicyMetadata{
			ResourceDomain: "docker", AutonomyClass: "low", Autonomous: true,
			Verifier: "docker_container_running",
		},
		ActionKey: store.AIControlActionDockerContainerRestart,
	}

	tests := []struct {
		name   string
		policy store.AIControlPolicy
		want   PolicyDecision
	}{
		{name: "default manual", policy: store.DefaultAIControlPolicy(), want: PolicyManual},
		{name: "paused", policy: store.AIControlPolicy{Mode: store.AIControlPaused, Revision: 2}, want: PolicyBlockedPaused},
		{name: "empty scope", policy: store.AIControlPolicy{Mode: store.AIControlLowRiskAuto, AllowedActions: []string{call.ActionKey}, Revision: 2}, want: PolicyBlockedScope},
		{name: "action denied", policy: store.AIControlPolicy{Mode: store.AIControlLowRiskAuto, NodeScope: []string{"node-1"}, Revision: 2}, want: PolicyBlockedAction},
		{name: "allowed", policy: store.AIControlPolicy{Mode: store.AIControlLowRiskAuto, AllowedActions: []string{call.ActionKey}, NodeScope: []string{"node-1"}, Revision: 2}, want: PolicyAutonomous},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := evaluatePolicy(test.policy, call); got.Decision != test.want {
				t.Fatalf("decision = %+v, want %s", got, test.want)
			}
		})
	}
}

func TestRegistryNeverAllowsUnclassifiedOrUnverifiedToolAutonomy(t *testing.T) {
	registry := &Registry{tools: make(map[string]registeredTool)}
	definition := noArgumentDefinition("unverified", "Unverified mutation")
	registry.add(registeredTool{
		definition: definition,
		risk:       RiskConfirm,
		metadata: PolicyMetadata{
			ResourceDomain: "docker", Action: store.AIControlActionDockerContainerStart,
			AutonomyClass: "low", Autonomous: true, Verifier: "missing",
		},
		validate: func(context.Context, json.RawMessage) (json.RawMessage, ToolTarget, error) {
			return json.RawMessage(`{}`), ToolTarget{NodeID: "node-1"}, nil
		},
	})
	registry.dependencies.Settings = fixedPolicyStore{policy: store.AIControlPolicy{
		Mode: store.AIControlLowRiskAuto, AllowedActions: []string{store.AIControlActionDockerContainerStart},
		NodeScope: []string{"node-1"}, Revision: 2,
	}}
	call, err := registry.Validate(t.Context(), definition.Name, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	decision, err := registry.Policy(t.Context(), call)
	if err != nil {
		t.Fatalf("policy: %v", err)
	}
	if decision.Decision != PolicyBlockedVerifier {
		t.Fatalf("decision = %+v, want blocked verifier", decision)
	}

	unclassified := &Registry{tools: make(map[string]registeredTool)}
	unclassified.add(registeredTool{definition: noArgumentDefinition("high_default", "Default high risk"), risk: RiskConfirm})
	tool := unclassified.tools["high_default"]
	if tool.metadata.AutonomyClass != "" || tool.metadata.Autonomous || tool.metadata.Verifier != "" {
		t.Fatalf("unclassified metadata = %+v", tool.metadata)
	}
}

func TestRegistryStopActionCannotInheritAutonomousRunningVerifier(t *testing.T) {
	tool := registeredTool{
		definition: noArgumentDefinition("action", "Action"), risk: RiskConfirm,
		metadata:  PolicyMetadata{ResourceDomain: "docker", Action: "docker.container", AutonomyClass: "low", Autonomous: true, Verifier: "running"},
		actionKey: func(json.RawMessage) string { return "docker.container.stop" },
		verify: func(context.Context, json.RawMessage, SafeToolResult, time.Time) VerificationResult {
			return VerificationResult{Status: VerificationSuccess}
		},
		validate: func(context.Context, json.RawMessage) (json.RawMessage, ToolTarget, error) {
			return json.RawMessage(`{}`), ToolTarget{NodeID: "node-1"}, nil
		},
	}
	registry := &Registry{tools: make(map[string]registeredTool)}
	registry.add(tool)
	call, err := registry.Validate(t.Context(), "action", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if call.Metadata.Autonomous || call.Metadata.Verifier != "" || call.Metadata.AutonomyClass != "high" {
		t.Fatalf("stop metadata = %+v", call.Metadata)
	}
}

type fixedPolicyStore struct {
	policy store.AIControlPolicy
}

func (s fixedPolicyStore) AIControlPolicy(context.Context) (store.AIControlPolicy, error) {
	return s.policy, nil
}
