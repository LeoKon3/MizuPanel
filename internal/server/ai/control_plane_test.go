package ai

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mizupanel/mizupanel/internal/protocol"
	"github.com/mizupanel/mizupanel/internal/server/store"
)

type controlPlaneTool struct {
	name     string
	action   string
	nodeID   string
	autonomy bool
	execute  func() (SafeToolResult, error)
	verify   func() VerificationResult
}

type verifierNodeOperations struct {
	platformNodeOperationsStub
	compose     protocol.DockerComposeListResponse
	composeWait bool
	systemd     protocol.SystemdServiceListResponse
	systemdWait bool
}

type sequencedPolicyStore struct {
	policy    store.AIControlPolicy
	failAfter int32
	calls     atomic.Int32
}

func (s *sequencedPolicyStore) AIControlPolicy(context.Context) (store.AIControlPolicy, error) {
	if s.calls.Add(1) > s.failAfter {
		return store.AIControlPolicy{}, errors.New("policy store unavailable")
	}
	return s.policy, nil
}

func (s verifierNodeOperations) DockerComposeList(ctx context.Context, _ string) (protocol.DockerComposeListResponse, error) {
	if s.composeWait {
		<-ctx.Done()
		return protocol.DockerComposeListResponse{}, ctx.Err()
	}
	return s.compose, nil
}

func (s verifierNodeOperations) SystemdServiceList(ctx context.Context, _ string) (protocol.SystemdServiceListResponse, error) {
	if s.systemdWait {
		<-ctx.Done()
		return protocol.SystemdServiceListResponse{}, ctx.Err()
	}
	return s.systemd, nil
}

func addControlPlaneTool(registry *Registry, spec controlPlaneTool) {
	metadata := PolicyMetadata{ResourceDomain: "docker", Action: spec.action, AutonomyClass: "high"}
	var verifier func(context.Context, json.RawMessage, SafeToolResult, time.Time) VerificationResult
	if spec.autonomy {
		metadata.AutonomyClass, metadata.Autonomous, metadata.Verifier = "low", true, "test_final_state"
		verifier = func(context.Context, json.RawMessage, SafeToolResult, time.Time) VerificationResult {
			return spec.verify()
		}
	}
	registry.add(registeredTool{
		definition: noArgumentDefinition(spec.name, "Control plane test mutation"),
		risk:       RiskConfirm,
		metadata:   metadata,
		verify:     verifier,
		validate: func(_ context.Context, raw json.RawMessage) (json.RawMessage, ToolTarget, error) {
			if err := strictArguments(raw, &struct{}{}); err != nil {
				return nil, ToolTarget{}, err
			}
			return json.RawMessage(`{}`), ToolTarget{Type: "node", ID: spec.nodeID, Name: spec.nodeID, NodeID: spec.nodeID}, nil
		},
		execute: func(context.Context, json.RawMessage) (SafeToolResult, error) { return spec.execute() },
	})
}

func newControlPlaneFixture(t *testing.T, tools []controlPlaneTool, policy store.AIControlPolicy) (*Service, *store.AIStore, *store.SettingsStore, store.AIConversation) {
	t.Helper()
	registry := &Registry{tools: make(map[string]registeredTool)}
	calls := make([]ToolCall, 0, len(tools))
	for _, tool := range tools {
		addControlPlaneTool(registry, tool)
		calls = append(calls, ToolCall{ID: "call-" + tool.name, Name: tool.name, Arguments: json.RawMessage(`{}`)})
	}
	adapter := serviceTestAdapter{complete: func(context.Context, ProviderCredential, ChatRequest) (ChatResponse, error) {
		return ChatResponse{ToolCalls: calls}, nil
	}}
	service, aiStore, database := newServiceTestFixture(t, registry, adapter)
	settings := store.NewSettingsStore(database)
	registry.dependencies.Settings = settings
	if _, err := settings.SetAIControlPolicy(t.Context(), policy); err != nil {
		t.Fatalf("set control policy: %v", err)
	}
	provider := createCapableServiceProvider(t, service, aiStore)
	conversation, err := service.CreateConversation(t.Context(), "Control Plane")
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	if conversation.ModelID == nil {
		modelID := provider.Models[0].ID
		if _, err := service.SetConversationModel(t.Context(), conversation.ID, &modelID); err != nil {
			t.Fatalf("set conversation model: %v", err)
		}
	}
	return service, aiStore, settings, conversation
}

func autoPolicy(actions ...string) store.AIControlPolicy {
	return store.AIControlPolicy{Mode: store.AIControlLowRiskAuto, AllowedActions: actions, NodeScope: []string{"node-1"}}
}

func TestServiceAutomaticallyExecutesAndVerifiesEligiblePlanOnce(t *testing.T) {
	var executions atomic.Int32
	service, _, _, conversation := newControlPlaneFixture(t, []controlPlaneTool{{
		name: "auto_restart", action: store.AIControlActionDockerContainerRestart, nodeID: "node-1", autonomy: true,
		execute: func() (SafeToolResult, error) {
			executions.Add(1)
			return SafeToolResult{Status: "success", Summary: "accepted response"}, nil
		},
		verify: func() VerificationResult {
			return VerificationResult{Status: VerificationSuccess, Summary: "服务已确认运行"}
		},
	}}, autoPolicy(store.AIControlActionDockerContainerRestart))

	result, err := service.Send(t.Context(), conversation.ID, "", "restart it", nil)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if executions.Load() != 1 || result.Plan == nil || result.Plan.Status != "success" || result.Message == nil {
		t.Fatalf("automatic result = %+v, executions=%d", result, executions.Load())
	}
	step := result.Plan.Steps[0]
	if step.PolicyDecision != string(PolicyAutonomous) || step.VerificationStatus != string(VerificationSuccess) ||
		!strings.Contains(result.Message.Content, "自动执行策略") {
		t.Fatalf("automatic step/message = %+v / %+v", step, result.Message)
	}
}

func TestServiceMixedPlanNeverPartiallyAutoExecutes(t *testing.T) {
	var executions atomic.Int32
	tools := []controlPlaneTool{
		{name: "eligible", action: store.AIControlActionDockerContainerStart, nodeID: "node-1", autonomy: true,
			execute: func() (SafeToolResult, error) { executions.Add(1); return SafeToolResult{Status: "success"}, nil },
			verify:  func() VerificationResult { return VerificationResult{Status: VerificationSuccess, Summary: "verified"} }},
		{name: "high_risk", action: "node.reboot", nodeID: "node-1", autonomy: false,
			execute: func() (SafeToolResult, error) { executions.Add(1); return SafeToolResult{Status: "success"}, nil }},
	}
	service, _, _, conversation := newControlPlaneFixture(t, tools, autoPolicy(store.AIControlActionDockerContainerStart))
	proposal, err := service.Send(t.Context(), conversation.ID, "", "run both", nil)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if executions.Load() != 0 || proposal.Plan == nil || proposal.Plan.Status != "pending" || proposal.Plan.PolicyDecision != string(PolicyManual) {
		t.Fatalf("proposal = %+v, executions=%d", proposal, executions.Load())
	}
	if len(proposal.Plan.Steps) != 2 || proposal.Plan.Steps[0].Impact.Version == 0 || !proposal.Plan.Impact.Available || proposal.Plan.Impact.Complete {
		t.Fatalf("proposal impact = %+v, want persisted available but incomplete snapshot", proposal.Plan.Impact)
	}
	confirmed, err := service.ConfirmPlan(t.Context(), proposal.Plan.ID, nil)
	if err != nil {
		t.Fatalf("ConfirmPlan: %v", err)
	}
	if executions.Load() != 2 || confirmed.Plan == nil || confirmed.Plan.Status != "success" {
		t.Fatalf("confirmed = %+v, executions=%d", confirmed, executions.Load())
	}
}

func TestServicePauseBlocksPendingConfirmationWithoutClaim(t *testing.T) {
	var executions atomic.Int32
	service, aiStore, settings, conversation := newControlPlaneFixture(t, []controlPlaneTool{{
		name: "manual", action: "node.reboot", nodeID: "node-1",
		execute: func() (SafeToolResult, error) { executions.Add(1); return SafeToolResult{Status: "success"}, nil },
	}}, store.DefaultAIControlPolicy())
	proposal, err := service.Send(t.Context(), conversation.ID, "", "run it", nil)
	if err != nil || proposal.Plan == nil {
		t.Fatalf("Send = %+v, %v", proposal, err)
	}
	if _, err := settings.SetAIControlPolicy(t.Context(), store.AIControlPolicy{Mode: store.AIControlPaused}); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if _, err := service.ConfirmPlan(t.Context(), proposal.Plan.ID, nil); !errors.Is(err, ErrAIControlPaused) {
		t.Fatalf("ConfirmPlan error = %v, want paused", err)
	}
	steps, err := aiStore.ListPlanSteps(t.Context(), proposal.Plan.ID)
	if err != nil || len(steps) != 1 || steps[0].Status != "pending" || executions.Load() != 0 {
		t.Fatalf("pending steps = %+v, executions=%d, err=%v", steps, executions.Load(), err)
	}
}

func TestServicePauseBetweenAutonomousStepsStopsFollowers(t *testing.T) {
	var first, second atomic.Int32
	var settings *store.SettingsStore
	tools := []controlPlaneTool{
		{name: "first", action: store.AIControlActionDockerContainerStart, nodeID: "node-1", autonomy: true,
			execute: func() (SafeToolResult, error) {
				first.Add(1)
				if _, err := settings.SetAIControlPolicy(t.Context(), store.AIControlPolicy{Mode: store.AIControlPaused}); err != nil {
					t.Fatalf("pause between steps: %v", err)
				}
				return SafeToolResult{Status: "success"}, nil
			}, verify: func() VerificationResult { return VerificationResult{Status: VerificationSuccess, Summary: "verified"} }},
		{name: "second", action: store.AIControlActionDockerContainerRestart, nodeID: "node-1", autonomy: true,
			execute: func() (SafeToolResult, error) { second.Add(1); return SafeToolResult{Status: "success"}, nil },
			verify:  func() VerificationResult { return VerificationResult{Status: VerificationSuccess, Summary: "verified"} }},
	}
	service, _, policyStore, conversation := newControlPlaneFixture(t, tools,
		autoPolicy(store.AIControlActionDockerContainerStart, store.AIControlActionDockerContainerRestart))
	settings = policyStore
	result, err := service.Send(t.Context(), conversation.ID, "", "run both", nil)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if first.Load() != 1 || second.Load() != 0 || result.Plan == nil || result.Plan.Status != "partial" || result.Message == nil ||
		!strings.Contains(result.Message.Content, "已暂停") {
		t.Fatalf("paused result = %+v, executions=%d/%d", result, first.Load(), second.Load())
	}
	if result.Plan.Steps[1].PolicyDecision != string(PolicyBlockedPaused) {
		t.Fatalf("blocked step = %+v", result.Plan.Steps[1])
	}
}

func TestAcceptedAutonomousPlanKeepsPolicyChecksForFollowingSteps(t *testing.T) {
	var first, second atomic.Int32
	service, aiStore, settings, conversation := newControlPlaneFixture(t, []controlPlaneTool{
		{
			name: "accepted", action: store.AIControlActionDockerContainerStart, nodeID: "node-1", autonomy: true,
			execute: func() (SafeToolResult, error) {
				first.Add(1)
				return SafeToolResult{Status: "accepted", Summary: "accepted", OperationID: "operation-1"}, nil
			},
			verify: func() VerificationResult { return VerificationResult{Status: VerificationSuccess, Summary: "verified"} },
		},
		{
			name: "follower", action: store.AIControlActionDockerContainerRestart, nodeID: "node-1", autonomy: true,
			execute: func() (SafeToolResult, error) {
				second.Add(1)
				return SafeToolResult{Status: "success"}, nil
			},
			verify: func() VerificationResult { return VerificationResult{Status: VerificationSuccess, Summary: "verified"} },
		},
	}, autoPolicy(store.AIControlActionDockerContainerStart, store.AIControlActionDockerContainerRestart))
	result, err := service.Send(t.Context(), conversation.ID, "", "run both", nil)
	if err != nil || result.Plan == nil || result.Plan.Status != "running" {
		t.Fatalf("Send = %+v, %v", result, err)
	}
	if first.Load() != 1 || second.Load() != 0 {
		t.Fatalf("initial executions = %d/%d", first.Load(), second.Load())
	}
	if _, err := settings.SetAIControlPolicy(t.Context(), autoPolicy(store.AIControlActionDockerContainerStart)); err != nil {
		t.Fatalf("tighten policy: %v", err)
	}
	if err := service.completeAcceptedOperation(t.Context(), result.Plan.Steps[0].ID, "success", "accepted operation verified", ""); err != nil {
		t.Fatalf("complete accepted operation: %v", err)
	}
	steps, err := aiStore.ListPlanSteps(t.Context(), result.Plan.ID)
	if err != nil {
		t.Fatalf("list plan steps: %v", err)
	}
	if second.Load() != 0 || len(steps) != 2 || steps[1].PolicyDecision != string(PolicyBlockedAction) || steps[1].Status != "failure" {
		t.Fatalf("following step = %+v, executions=%d", steps, second.Load())
	}
}

func TestServiceUnknownVerificationNeverRetries(t *testing.T) {
	var executions atomic.Int32
	service, _, _, conversation := newControlPlaneFixture(t, []controlPlaneTool{{
		name: "unknown", action: store.AIControlActionDockerContainerRestart, nodeID: "node-1", autonomy: true,
		execute: func() (SafeToolResult, error) { executions.Add(1); return SafeToolResult{Status: "success"}, nil },
		verify: func() VerificationResult {
			return VerificationResult{Status: VerificationUnknown, Summary: "状态证据不足"}
		},
	}}, autoPolicy(store.AIControlActionDockerContainerRestart))
	result, err := service.Send(t.Context(), conversation.ID, "", "restart it", nil)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if executions.Load() != 1 || result.Plan == nil || result.Plan.Steps[0].Status != "interrupted" ||
		result.Plan.Steps[0].VerificationStatus != string(VerificationUnknown) || result.Message == nil ||
		!strings.Contains(result.Message.Content, "未自动重试") {
		t.Fatalf("unknown result = %+v, executions=%d", result, executions.Load())
	}
}

func TestServiceAutonomousAndManualClaimsExecutePlanOnlyOnce(t *testing.T) {
	var executions atomic.Int32
	service, aiStore, settings, conversation := newControlPlaneFixture(t, []controlPlaneTool{{
		name: "race", action: store.AIControlActionDockerContainerRestart, nodeID: "node-1", autonomy: true,
		execute: func() (SafeToolResult, error) {
			executions.Add(1)
			return SafeToolResult{Status: "success"}, nil
		},
		verify: func() VerificationResult {
			return VerificationResult{Status: VerificationSuccess, Summary: "verified"}
		},
	}}, store.DefaultAIControlPolicy())
	proposal, err := service.Send(t.Context(), conversation.ID, "", "restart it", nil)
	if err != nil || proposal.Plan == nil {
		t.Fatalf("Send = %+v, %v", proposal, err)
	}
	if _, err := settings.SetAIControlPolicy(t.Context(), autoPolicy(store.AIControlActionDockerContainerRestart)); err != nil {
		t.Fatalf("enable autonomous policy: %v", err)
	}

	start := make(chan struct{})
	errorsCh := make(chan error, 2)
	go func() {
		<-start
		_, _, err := service.tryClaimAutonomousPlan(t.Context(), proposal.Plan.ID, nil)
		errorsCh <- err
	}()
	go func() {
		<-start
		_, err := service.ConfirmPlan(t.Context(), proposal.Plan.ID, nil)
		errorsCh <- err
	}()
	close(start)

	successes, conflicts := 0, 0
	for range 2 {
		err := <-errorsCh
		switch {
		case err == nil:
			successes++
		case errors.Is(err, store.ErrAIConflict):
			conflicts++
		default:
			t.Fatalf("claim error = %v", err)
		}
	}
	steps, err := aiStore.ListPlanSteps(t.Context(), proposal.Plan.ID)
	if err != nil {
		t.Fatalf("list steps: %v", err)
	}
	if successes != 1 || conflicts != 1 || executions.Load() != 1 || len(steps) != 1 || steps[0].Status != "success" {
		t.Fatalf("claims success/conflict=%d/%d, executions=%d, steps=%+v", successes, conflicts, executions.Load(), steps)
	}
}

func TestServicePolicyFailureAfterAutonomousClaimStopsPlan(t *testing.T) {
	var executions atomic.Int32
	service, aiStore, _, conversation := newControlPlaneFixture(t, []controlPlaneTool{{
		name: "policy_failure", action: store.AIControlActionDockerContainerRestart, nodeID: "node-1", autonomy: true,
		execute: func() (SafeToolResult, error) {
			executions.Add(1)
			return SafeToolResult{Status: "success"}, nil
		},
		verify: func() VerificationResult {
			return VerificationResult{Status: VerificationSuccess, Summary: "verified"}
		},
	}}, store.DefaultAIControlPolicy())
	proposal, err := service.Send(t.Context(), conversation.ID, "", "restart it", nil)
	if err != nil || proposal.Plan == nil {
		t.Fatalf("Send = %+v, %v", proposal, err)
	}
	service.registry.dependencies.Settings = &sequencedPolicyStore{
		policy:    autoPolicy(store.AIControlActionDockerContainerRestart),
		failAfter: 1,
	}

	result, claimed, err := service.tryClaimAutonomousPlan(t.Context(), proposal.Plan.ID, nil)
	if err != nil || !claimed {
		t.Fatalf("tryClaimAutonomousPlan = %+v, claimed=%t, err=%v", result, claimed, err)
	}
	steps, listErr := aiStore.ListPlanSteps(t.Context(), proposal.Plan.ID)
	turn, turnErr := aiStore.GetTurn(t.Context(), proposal.Plan.ID)
	if listErr != nil || turnErr != nil {
		t.Fatalf("load stopped plan: steps=%v turn=%v", listErr, turnErr)
	}
	if executions.Load() != 0 || len(steps) != 1 || steps[0].Status != "failure" ||
		steps[0].PolicyDecision != string(PolicyBlockedPolicy) || steps[0].PolicyReason != "policy_unavailable" ||
		turn.Status != "completed" || result.Plan == nil || result.Message == nil {
		t.Fatalf("stopped plan = result:%+v turn:%+v steps:%+v executions=%d", result, turn, steps, executions.Load())
	}
}

func TestDockerVerifierWaitsForFreshPostOperationSnapshot(t *testing.T) {
	registry := NewRegistry(RegistryDependencies{})
	_, _, database := newServiceTestFixture(t, registry, serviceTestAdapter{})
	nodes := store.NewNodeStore(database)
	docker := store.NewDockerSnapshotStore(database)
	registry.dependencies.Docker = docker
	if err := nodes.Upsert(t.Context(), store.Node{ID: "node-1", Name: "node-1", Status: "online", LastSeenAt: time.Now().UTC()}); err != nil {
		t.Fatalf("upsert node: %v", err)
	}

	notBefore := time.Now().UTC()
	if err := docker.Upsert(t.Context(), "node-1", protocol.DockerSnapshot{
		CollectedAt: notBefore.Unix(), Available: true,
		Containers: []protocol.ContainerInfo{{ID: "container-1", State: "exited"}},
	}); err != nil {
		t.Fatalf("upsert stale snapshot: %v", err)
	}
	freshResult := make(chan error, 1)
	go func() {
		time.Sleep(250 * time.Millisecond)
		freshResult <- docker.Upsert(t.Context(), "node-1", protocol.DockerSnapshot{
			CollectedAt: notBefore.Unix() + 1, Available: true,
			Containers: []protocol.ContainerInfo{{ID: "container-1", State: "running"}},
		})
	}()

	started := time.Now()
	result := registry.verifyDockerRunning(t.Context(), json.RawMessage(`{"node_id":"node-1","container_id":"container-1"}`),
		SafeToolResult{Status: "success"}, notBefore)
	if err := <-freshResult; err != nil {
		t.Fatalf("upsert fresh snapshot: %v", err)
	}
	if result.Status != VerificationSuccess || time.Since(started) < 200*time.Millisecond {
		t.Fatalf("verification = %+v after %s, want fresh snapshot success", result, time.Since(started))
	}

	if err := docker.Upsert(t.Context(), "node-1", protocol.DockerSnapshot{
		CollectedAt: notBefore.Unix() + 2, Available: true,
		Containers: []protocol.ContainerInfo{{ID: "container-1", State: "exited"}},
	}); err != nil {
		t.Fatalf("upsert failed snapshot: %v", err)
	}
	failed := registry.verifyDockerRunning(t.Context(), json.RawMessage(`{"node_id":"node-1","container_id":"container-1"}`),
		SafeToolResult{Status: "success"}, time.Time{})
	if failed.Status != VerificationFailure {
		t.Fatalf("failed verification = %+v", failed)
	}

	if err := docker.Upsert(t.Context(), "node-1", protocol.DockerSnapshot{
		CollectedAt: notBefore.Unix() + 3, Available: true,
		Containers: []protocol.ContainerInfo{{ID: "other", State: "running"}},
	}); err != nil {
		t.Fatalf("upsert missing snapshot: %v", err)
	}
	unknown := registry.verifyDockerRunning(t.Context(), json.RawMessage(`{"node_id":"node-1","container_id":"container-1"}`),
		SafeToolResult{Status: "success"}, time.Time{})
	if unknown.Status != VerificationUnknown {
		t.Fatalf("missing verification = %+v", unknown)
	}

	staleAt := time.Now().UTC()
	if err := docker.Upsert(t.Context(), "node-1", protocol.DockerSnapshot{
		CollectedAt: staleAt.Unix(), Available: true,
		Containers: []protocol.ContainerInfo{{ID: "container-1", State: "running"}},
	}); err != nil {
		t.Fatalf("upsert timeout snapshot: %v", err)
	}
	timeoutCtx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	timedOut := registry.verifyDockerRunning(timeoutCtx, json.RawMessage(`{"node_id":"node-1","container_id":"container-1"}`),
		SafeToolResult{Status: "success"}, staleAt)
	if timedOut.Status != VerificationUnknown {
		t.Fatalf("timeout verification = %+v", timedOut)
	}
}

func TestComposeAndSystemdVerifiersClassifyFinalState(t *testing.T) {
	composeArgs := json.RawMessage(`{"node_id":"node-1","project_name":"panel","service_name":"server"}`)
	systemdArgs := json.RawMessage(`{"node_id":"node-1","service_name":"nginx.service"}`)
	tests := []struct {
		name       string
		operations verifierNodeOperations
		verify     func(*Registry) VerificationResult
		want       VerificationStatus
	}{
		{
			name: "compose success",
			operations: verifierNodeOperations{compose: protocol.DockerComposeListResponse{Success: true, Supported: true,
				Projects: []protocol.DockerComposeProject{{Name: "panel", Services: []protocol.DockerComposeService{{Name: "server", State: "running"}}}}}},
			verify: func(registry *Registry) VerificationResult {
				return registry.verifyComposeRunning(t.Context(), composeArgs, SafeToolResult{Status: "success"}, time.Time{})
			},
			want: VerificationSuccess,
		},
		{
			name: "compose failure",
			operations: verifierNodeOperations{compose: protocol.DockerComposeListResponse{Success: true, Supported: true,
				Projects: []protocol.DockerComposeProject{{Name: "panel", Services: []protocol.DockerComposeService{{Name: "server", State: "exited"}}}}}},
			verify: func(registry *Registry) VerificationResult {
				return registry.verifyComposeRunning(t.Context(), composeArgs, SafeToolResult{Status: "success"}, time.Time{})
			},
			want: VerificationFailure,
		},
		{
			name:       "compose unknown",
			operations: verifierNodeOperations{compose: protocol.DockerComposeListResponse{Success: true, Supported: true, Projects: []protocol.DockerComposeProject{}}},
			verify: func(registry *Registry) VerificationResult {
				return registry.verifyComposeRunning(t.Context(), composeArgs, SafeToolResult{Status: "success"}, time.Time{})
			},
			want: VerificationUnknown,
		},
		{
			name: "systemd success",
			operations: verifierNodeOperations{systemd: protocol.SystemdServiceListResponse{Success: true, Supported: true,
				Services: []protocol.SystemdService{{Name: "nginx.service", ActiveState: "active"}}}},
			verify: func(registry *Registry) VerificationResult {
				return registry.verifySystemdRunning(t.Context(), systemdArgs, SafeToolResult{Status: "success"}, time.Time{})
			},
			want: VerificationSuccess,
		},
		{
			name: "systemd failure",
			operations: verifierNodeOperations{systemd: protocol.SystemdServiceListResponse{Success: true, Supported: true,
				Services: []protocol.SystemdService{{Name: "nginx.service", ActiveState: "inactive"}}}},
			verify: func(registry *Registry) VerificationResult {
				return registry.verifySystemdRunning(t.Context(), systemdArgs, SafeToolResult{Status: "success"}, time.Time{})
			},
			want: VerificationFailure,
		},
		{
			name:       "systemd unknown",
			operations: verifierNodeOperations{systemd: protocol.SystemdServiceListResponse{Success: true, Supported: true, Services: []protocol.SystemdService{}}},
			verify: func(registry *Registry) VerificationResult {
				return registry.verifySystemdRunning(t.Context(), systemdArgs, SafeToolResult{Status: "success"}, time.Time{})
			},
			want: VerificationUnknown,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registry := NewRegistry(RegistryDependencies{AgentOps: test.operations})
			if got := test.verify(registry); got.Status != test.want {
				t.Fatalf("verification = %+v, want %s", got, test.want)
			}
		})
	}
}

func TestLiveVerifierTimeoutsAndCancellationRemainUnknown(t *testing.T) {
	composeRegistry := NewRegistry(RegistryDependencies{AgentOps: verifierNodeOperations{composeWait: true}})
	composeCtx, cancelCompose := context.WithTimeout(t.Context(), 25*time.Millisecond)
	defer cancelCompose()
	compose := composeRegistry.verifyComposeRunning(composeCtx,
		json.RawMessage(`{"node_id":"node-1","project_name":"panel","service_name":"server"}`),
		SafeToolResult{Status: "success"}, time.Time{})
	if compose.Status != VerificationUnknown {
		t.Fatalf("compose timeout = %+v", compose)
	}

	systemdRegistry := NewRegistry(RegistryDependencies{AgentOps: verifierNodeOperations{systemdWait: true}})
	systemdCtx, cancelSystemd := context.WithTimeout(t.Context(), 25*time.Millisecond)
	defer cancelSystemd()
	systemd := systemdRegistry.verifySystemdRunning(systemdCtx,
		json.RawMessage(`{"node_id":"node-1","service_name":"nginx.service"}`),
		SafeToolResult{Status: "success"}, time.Time{})
	if systemd.Status != VerificationUnknown {
		t.Fatalf("systemd timeout = %+v", systemd)
	}

	registry := &Registry{tools: map[string]registeredTool{
		"cancelled": {
			definition: noArgumentDefinition("cancelled", "cancelled verifier"),
			metadata:   PolicyMetadata{Verifier: "test"},
			verify: func(context.Context, json.RawMessage, SafeToolResult, time.Time) VerificationResult {
				t.Fatal("verifier ran after cancellation")
				return VerificationResult{}
			},
		},
	}}
	cancelledCtx, cancel := context.WithCancel(t.Context())
	cancel()
	cancelled := registry.Verify(cancelledCtx, ValidatedToolCall{Definition: ToolDefinition{Name: "cancelled"}}, SafeToolResult{}, time.Time{})
	if cancelled.Status != VerificationUnknown {
		t.Fatalf("cancelled verification = %+v", cancelled)
	}
}
