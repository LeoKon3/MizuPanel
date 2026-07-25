package docker

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/mizupanel/mizupanel/internal/protocol"
	"gopkg.in/yaml.v3"
)

const (
	defaultManagedComposeRoot   = "/var/lib/mizupanel/compose"
	managedComposeRootMode      = 0750
	managedComposeProjectMode   = 0700
	managedComposeFileMode      = 0600
	managedComposeRevisionLimit = 5

	maxManagedComposeYAMLBytes = 1 << 20
	maxManagedComposeEnvBytes  = 256 << 10
	maxManagedDisplayNameBytes = 128

	managedComposeConfirmationTTL  = 5 * time.Minute
	maxManagedComposeConfirmations = 128
)

type composeDeploymentConfirmation struct {
	ProjectID   string
	Fingerprint string
	ExpiresAt   time.Time
}

type managedComposeMetadata struct {
	ID                 string `json:"id"`
	DisplayName        string `json:"display_name"`
	ComposeProjectName string `json:"compose_project_name"`
	Revision           int    `json:"revision"`
	UpdatedAt          string `json:"updated_at"`
}

type managedComposePaths struct {
	root         string
	projectDir   string
	composeFile  string
	envFile      string
	metadataFile string
	revisionsDir string
}

type managedComposeDraft struct {
	ProjectID   string
	DisplayName string
	ComposeYAML string
	EnvFile     string
	PullImages  bool
}

type managedComposeRevision struct {
	Number int
	Path   string
}

type managedComposeFileBackup struct {
	exists bool
	data   []byte
}

type deploymentSafeError struct {
	message string
}

func (e deploymentSafeError) Error() string {
	return e.message
}

func safeDeploymentError(message string) error {
	return deploymentSafeError{message: message}
}

func deploymentResponseError(err error) string {
	var safeErr deploymentSafeError
	if errors.As(err, &safeErr) {
		return sanitizeComposeValidationOutput(safeErr.message)
	}
	return "托管 Compose 操作失败"
}

// HandleDockerComposeDeployment handles only Agent-owned Compose projects.
// Neither a user-supplied path nor arbitrary Docker Compose arguments can
// reach this method's command runner.
func (h *ComposeHandler) HandleDockerComposeDeployment(ctx context.Context, req protocol.DockerComposeDeploymentRequest) protocol.DockerComposeDeploymentResponse {
	supported := h.SupportsDeployment()
	response := protocol.DockerComposeDeploymentResponse{
		Type:      protocol.MessageTypeDockerComposeDeploymentResponse,
		RequestID: req.RequestID,
		Supported: supported,
		Action:    strings.TrimSpace(req.Action),
		Risks:     []protocol.DockerComposeRisk{},
	}
	if h == nil || !h.supported || h.runner == nil {
		response.Error = "Docker Compose CLI 不可用"
		return response
	}
	if !supported {
		response.Error = "托管 Compose 存储目录不可用"
		return response
	}

	requestCtx, cancel := context.WithTimeout(ctx, composeActionTimeout)
	defer cancel()

	switch response.Action {
	case "preview":
		return h.handleManagedComposePreview(requestCtx, req, response)
	case "apply":
		return h.handleManagedComposeApply(requestCtx, req, response)
	case "rollback":
		return h.handleManagedComposeRollback(requestCtx, req, response)
	case "archive":
		return h.handleManagedComposeArchive(requestCtx, req, response)
	default:
		response.Error = "不支持的托管 Compose 操作"
		return response
	}
}

func (h *ComposeHandler) handleManagedComposePreview(ctx context.Context, req protocol.DockerComposeDeploymentRequest, response protocol.DockerComposeDeploymentResponse) protocol.DockerComposeDeploymentResponse {
	draft, err := normalizeManagedComposeDraft(req, false)
	if err != nil {
		response.Error = deploymentResponseError(err)
		return response
	}

	existingProject := draft.ProjectID != ""
	if !existingProject {
		draft.ProjectID = uuid.NewString()
	}
	operationKey := managedComposeProjectName(draft.ProjectID)
	if !h.beginComposeOperation(operationKey) {
		response.Error = "该 Compose 项目已有操作正在执行"
		return response
	}
	defer h.finishComposeOperation(operationKey)

	var metadata managedComposeMetadata
	var paths managedComposePaths
	if existingProject {
		metadata, paths, err = h.loadManagedComposeProject(draft.ProjectID)
		if err != nil {
			response.Error = "托管 Compose 项目不存在或状态无效"
			return response
		}
	}

	risks, output, err := h.validateManagedComposeDraft(ctx, draft)
	response.Risks = risks
	response.Output = output
	if err != nil {
		response.Error = deploymentResponseError(err)
		return response
	}

	token, err := h.rememberManagedComposeConfirmation(draft)
	if err != nil {
		response.Error = "生成部署确认令牌失败"
		return response
	}
	if metadata.ID == "" {
		metadata = managedComposeMetadata{
			ID:                 draft.ProjectID,
			DisplayName:        draft.DisplayName,
			ComposeProjectName: managedComposeProjectName(draft.ProjectID),
		}
	} else {
		metadata.DisplayName = draft.DisplayName
	}
	response.Project = managedComposeProject(metadata, managedComposeRollbackAvailable(paths, metadata.Revision))
	response.ConfirmationToken = token
	response.Success = true
	return response
}

func (h *ComposeHandler) handleManagedComposeApply(ctx context.Context, req protocol.DockerComposeDeploymentRequest, response protocol.DockerComposeDeploymentResponse) protocol.DockerComposeDeploymentResponse {
	draft, err := normalizeManagedComposeDraft(req, true)
	if err != nil {
		response.Error = deploymentResponseError(err)
		return response
	}
	operationKey := managedComposeProjectName(draft.ProjectID)
	if !h.beginComposeOperation(operationKey) {
		response.Error = "该 Compose 项目已有操作正在执行"
		return response
	}
	defer h.finishComposeOperation(operationKey)

	risks, output, err := h.validateManagedComposeDraft(ctx, draft)
	response.Risks = risks
	response.Output = output
	if err != nil {
		response.Error = deploymentResponseError(err)
		return response
	}
	if !h.consumeManagedComposeConfirmation(strings.TrimSpace(req.ConfirmationToken), draft) {
		response.Error = "部署确认令牌无效、已过期或与预览内容不一致"
		return response
	}

	metadata, paths, existing, err := h.prepareManagedComposeProject(draft.ProjectID)
	if err != nil {
		response.Error = deploymentResponseError(err)
		return response
	}
	previousMetadata := metadata
	metadata = managedComposeMetadata{
		ID:                 draft.ProjectID,
		DisplayName:        draft.DisplayName,
		ComposeProjectName: managedComposeProjectName(draft.ProjectID),
		Revision:           previousMetadata.Revision + 1,
		UpdatedAt:          time.Now().UTC().Format(time.RFC3339),
	}

	revisionPath, err := writeManagedComposeRevision(paths, metadata.Revision, []byte(draft.ComposeYAML))
	if err != nil {
		response.Error = deploymentResponseError(err)
		return response
	}
	restore, err := replaceManagedComposeFiles(paths, []byte(draft.ComposeYAML), []byte(draft.EnvFile), metadata)
	if err != nil {
		_ = removeManagedComposeFile(revisionPath)
		response.Error = deploymentResponseError(err)
		return response
	}

	outputs := make([]string, 0, 2)
	if draft.PullImages {
		output, runErr := h.runManagedCompose(ctx, metadata, paths, draft, "pull")
		if output != "" {
			outputs = append(outputs, output)
		}
		if runErr != nil {
			restoreErr := h.restoreManagedComposeAfterFailure(ctx, previousMetadata, paths, existing, restore, draft)
			_ = removeManagedComposeFile(revisionPath)
			if !existing {
				cleanupEmptyManagedComposeProject(paths)
			}
			response.Output = boundedComposeOutput(strings.Join(outputs, "\n"), "")
			if restoreErr != nil {
				response.Error = deploymentResponseError(restoreErr)
			} else {
				response.Error = deploymentResponseError(runErr)
			}
			return response
		}
	}
	output, runErr := h.runManagedCompose(ctx, metadata, paths, draft, "up", "-d")
	if output != "" {
		outputs = append(outputs, output)
	}
	if runErr != nil {
		restoreErr := h.restoreManagedComposeAfterFailure(ctx, previousMetadata, paths, existing, restore, draft)
		_ = removeManagedComposeFile(revisionPath)
		if !existing {
			cleanupEmptyManagedComposeProject(paths)
		}
		response.Output = boundedComposeOutput(strings.Join(outputs, "\n"), "")
		if restoreErr != nil {
			response.Error = deploymentResponseError(restoreErr)
		} else {
			response.Error = deploymentResponseError(runErr)
		}
		return response
	}
	if err := pruneManagedComposeRevisions(paths); err != nil {
		response.Error = deploymentResponseError(err)
		return response
	}

	response.Project = managedComposeProject(metadata, managedComposeRollbackAvailable(paths, metadata.Revision))
	response.Output = boundedComposeOutput(strings.Join(outputs, "\n"), "")
	response.Success = true
	return response
}

func normalizeManagedComposeDraft(req protocol.DockerComposeDeploymentRequest, requireProjectID bool) (managedComposeDraft, error) {
	draft := managedComposeDraft{
		ProjectID:   strings.TrimSpace(req.ProjectID),
		DisplayName: strings.TrimSpace(req.DisplayName),
		ComposeYAML: req.ComposeYAML,
		EnvFile:     req.EnvFile,
		PullImages:  req.PullImages,
	}
	if requireProjectID && !validManagedComposeProjectID(draft.ProjectID) {
		return managedComposeDraft{}, safeDeploymentError("托管 Compose 项目标识无效")
	}
	if !requireProjectID && draft.ProjectID != "" && !validManagedComposeProjectID(draft.ProjectID) {
		return managedComposeDraft{}, safeDeploymentError("托管 Compose 项目标识无效")
	}
	if !validManagedComposeDisplayName(draft.DisplayName) {
		return managedComposeDraft{}, safeDeploymentError("应用显示名称无效")
	}
	if len(draft.ComposeYAML) == 0 || len(draft.ComposeYAML) > maxManagedComposeYAMLBytes || !utf8.ValidString(draft.ComposeYAML) {
		return managedComposeDraft{}, safeDeploymentError("Compose YAML 内容无效或超过大小限制")
	}
	if len(draft.EnvFile) > maxManagedComposeEnvBytes || !utf8.ValidString(draft.EnvFile) || strings.ContainsRune(draft.EnvFile, '\x00') {
		return managedComposeDraft{}, safeDeploymentError(".env 内容无效或超过大小限制")
	}
	if managedComposeEnvControlsCLI(draft.EnvFile) {
		return managedComposeDraft{}, safeDeploymentError(".env 不支持 COMPOSE_ 控制变量")
	}
	return draft, nil
}

func managedComposeEnvControlsCLI(source string) bool {
	for _, rawLine := range strings.Split(source, "\n") {
		line := strings.TrimSpace(strings.TrimPrefix(rawLine, "\ufeff"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		key := line
		if separator := strings.IndexByte(key, '='); separator >= 0 {
			key = key[:separator]
		}
		if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(key)), "COMPOSE_") {
			return true
		}
	}
	return false
}

func validManagedComposeDisplayName(name string) bool {
	if name == "" || len(name) > maxManagedDisplayNameBytes || !utf8.ValidString(name) {
		return false
	}
	for _, value := range name {
		if value == '\x00' || unicode.IsControl(value) {
			return false
		}
	}
	return true
}

func validManagedComposeProjectID(id string) bool {
	parsed, err := uuid.Parse(id)
	return err == nil && parsed != uuid.Nil && parsed.String() == id
}

func managedComposeProjectName(id string) string {
	return "mizupanel-" + id
}

func looksLikeManagedComposeProjectName(name string) bool {
	const prefix = "mizupanel-"
	if !strings.HasPrefix(name, prefix) {
		return false
	}
	id := strings.TrimPrefix(name, prefix)
	return validManagedComposeProjectID(id) && managedComposeProjectName(id) == name
}

func managedComposeProject(metadata managedComposeMetadata, rollbackAvailable bool) protocol.DockerComposeProject {
	return protocol.DockerComposeProject{
		Name:              metadata.ComposeProjectName,
		Services:          []protocol.DockerComposeService{},
		Management:        "managed",
		ManagedProjectID:  metadata.ID,
		DisplayName:       metadata.DisplayName,
		Revision:          metadata.Revision,
		RollbackAvailable: rollbackAvailable,
	}
}

func (h *ComposeHandler) validateManagedComposeDraft(ctx context.Context, draft managedComposeDraft) ([]protocol.DockerComposeRisk, string, error) {
	risks, err := analyzeManagedComposeYAML(draft.ComposeYAML)
	if err != nil {
		return risks, "", err
	}
	root, err := h.ensureManagedComposeRoot(true)
	if err != nil {
		return risks, "", err
	}
	paths, cleanup, err := stageManagedComposeDraft(root, draft)
	if err != nil {
		return risks, "", err
	}
	defer cleanup()

	if h.runner == nil {
		return risks, "", safeDeploymentError("Docker Compose CLI 不可用")
	}
	args := []string{"compose", "--project-name", managedComposeProjectName(draft.ProjectID), "--file", paths.composeFile, "--env-file", paths.envFile, "config", "--quiet"}
	stdout, stderr, runErr := h.runner(ctx, args...)
	output := h.redactedManagedComposeOutput(stdout, stderr, draft)
	if runErr != nil {
		return risks, output, safeDeploymentError("Compose 配置校验失败")
	}
	return risks, output, nil
}

func stageManagedComposeDraft(root string, draft managedComposeDraft) (managedComposePaths, func(), error) {
	stagingDir, err := os.MkdirTemp(root, ".compose-staging-")
	if err != nil {
		return managedComposePaths{}, func() {}, safeDeploymentError("创建 Compose 校验暂存目录失败")
	}
	if err := os.Chmod(stagingDir, managedComposeProjectMode); err != nil {
		_ = os.RemoveAll(stagingDir)
		return managedComposePaths{}, func() {}, safeDeploymentError("设置 Compose 校验暂存目录权限失败")
	}
	paths := managedComposePaths{
		root:        root,
		projectDir:  stagingDir,
		composeFile: filepath.Join(stagingDir, "compose.yaml"),
		envFile:     filepath.Join(stagingDir, ".env"),
	}
	if err := writeManagedComposeFile(paths.composeFile, []byte(draft.ComposeYAML)); err != nil {
		_ = os.RemoveAll(stagingDir)
		return managedComposePaths{}, func() {}, err
	}
	if err := writeManagedComposeFile(paths.envFile, []byte(draft.EnvFile)); err != nil {
		_ = os.RemoveAll(stagingDir)
		return managedComposePaths{}, func() {}, err
	}
	return paths, func() { _ = os.RemoveAll(stagingDir) }, nil
}

func (h *ComposeHandler) redactedManagedComposeOutput(stdout string, stderr string, _ managedComposeDraft) string {
	// Compose diagnostics can echo environment values, resolved configuration, and
	// absolute paths.  Managed deployments are intentionally write-only: never
	// return command output to a caller, even after sanitization.
	_ = stdout
	_ = stderr
	return "操作未返回诊断输出。"
}

func (h *ComposeHandler) rememberManagedComposeConfirmation(draft managedComposeDraft) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	h.tokenMu.Lock()
	defer h.tokenMu.Unlock()
	if h.confirmations == nil {
		h.confirmations = make(map[string]composeDeploymentConfirmation)
	}
	now := time.Now()
	for key, confirmation := range h.confirmations {
		if !confirmation.ExpiresAt.After(now) {
			delete(h.confirmations, key)
		}
	}
	if len(h.confirmations) >= maxManagedComposeConfirmations {
		var oldestToken string
		var oldestExpiry time.Time
		for candidate, confirmation := range h.confirmations {
			if oldestToken == "" || confirmation.ExpiresAt.Before(oldestExpiry) {
				oldestToken = candidate
				oldestExpiry = confirmation.ExpiresAt
			}
		}
		delete(h.confirmations, oldestToken)
	}
	h.confirmations[token] = composeDeploymentConfirmation{ProjectID: draft.ProjectID, Fingerprint: managedComposeDraftFingerprint(draft), ExpiresAt: now.Add(managedComposeConfirmationTTL)}
	return token, nil
}

func (h *ComposeHandler) consumeManagedComposeConfirmation(token string, draft managedComposeDraft) bool {
	if token == "" {
		return false
	}
	h.tokenMu.Lock()
	defer h.tokenMu.Unlock()
	confirmation, exists := h.confirmations[token]
	if !exists || !confirmation.ExpiresAt.After(time.Now()) || confirmation.ProjectID != draft.ProjectID || confirmation.Fingerprint != managedComposeDraftFingerprint(draft) {
		if exists && !confirmation.ExpiresAt.After(time.Now()) {
			delete(h.confirmations, token)
		}
		return false
	}
	delete(h.confirmations, token)
	return true
}

func managedComposeDraftFingerprint(draft managedComposeDraft) string {
	input := strings.Join([]string{draft.ProjectID, draft.DisplayName, draft.ComposeYAML, draft.EnvFile, strconv.FormatBool(draft.PullImages)}, "\x00")
	sum := sha256.Sum256([]byte(input))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func analyzeManagedComposeYAML(source string) ([]protocol.DockerComposeRisk, error) {
	decoder := yaml.NewDecoder(strings.NewReader(source))
	var document yaml.Node
	if err := decoder.Decode(&document); err != nil {
		return nil, safeDeploymentError("Compose YAML 格式无效")
	}
	var additional yaml.Node
	if err := decoder.Decode(&additional); err != io.EOF {
		return nil, safeDeploymentError("Compose YAML 只能包含一个文档")
	}
	root := unwrapComposeYAMLNode(&document)
	if root == nil || root.Kind != yaml.MappingNode {
		return nil, safeDeploymentError("Compose YAML 顶层必须是映射")
	}
	services := composeYAMLMappingValue(root, "services")
	services = unwrapComposeYAMLNode(services)
	if services == nil || services.Kind != yaml.MappingNode || len(services.Content) == 0 {
		return nil, safeDeploymentError("Compose YAML 必须包含服务定义")
	}
	if composeYAMLMappingValue(root, "include") != nil {
		return nil, safeDeploymentError("托管 Compose 部署不支持 include 外部配置")
	}
	if composeYAMLContainsKey(composeYAMLMappingValue(root, "configs"), "file", make(map[*yaml.Node]struct{})) || composeYAMLContainsKey(composeYAMLMappingValue(root, "secrets"), "file", make(map[*yaml.Node]struct{})) {
		return nil, safeDeploymentError("托管 Compose 部署不支持外部配置文件")
	}

	risks := make([]protocol.DockerComposeRisk, 0, 5)
	seenRisks := make(map[string]struct{})
	for index := 0; index+1 < len(services.Content); index += 2 {
		service := unwrapComposeYAMLNode(services.Content[index+1])
		if service == nil || service.Kind != yaml.MappingNode {
			return nil, safeDeploymentError("Compose 服务定义无效")
		}
		if composeYAMLContainsKey(service, "build", make(map[*yaml.Node]struct{})) {
			return nil, safeDeploymentError("托管 Compose 部署不支持 build")
		}
		if composeYAMLMappingValue(service, "extends") != nil {
			return nil, safeDeploymentError("托管 Compose 部署不支持 extends")
		}
		if composeYAMLMappingValue(service, "env_file") != nil {
			return nil, safeDeploymentError("托管 Compose 部署不支持 YAML env_file")
		}
		if composeYAMLMappingValue(service, "profiles") != nil {
			return nil, safeDeploymentError("托管 Compose 部署不支持 profiles")
		}
		privileged := composeYAMLMappingValue(service, "privileged")
		if composeYAMLBoolean(privileged) {
			addManagedComposeRisk(&risks, seenRisks, "privileged", "high", "服务启用了 privileged 权限")
		} else if composeYAMLUsesInterpolation(privileged) {
			addManagedComposeRisk(&risks, seenRisks, "privileged", "high", "服务的 privileged 设置使用动态变量，可能启用特权权限")
		}
		networkMode := composeYAMLMappingValue(service, "network_mode")
		if strings.EqualFold(strings.TrimSpace(composeYAMLScalar(networkMode)), "host") {
			addManagedComposeRisk(&risks, seenRisks, "host_network", "high", "服务使用了宿主机网络")
		} else if composeYAMLUsesInterpolation(networkMode) {
			addManagedComposeRisk(&risks, seenRisks, "host_network", "high", "服务的网络模式使用动态变量，可能启用宿主机网络")
		}
		if composeYAMLHasEntries(composeYAMLMappingValue(service, "devices")) {
			addManagedComposeRisk(&risks, seenRisks, "devices", "high", "服务声明了宿主机设备访问")
		}
		analyzeManagedComposeVolumes(composeYAMLMappingValue(service, "volumes"), &risks, seenRisks)
	}
	analyzeManagedComposeTopLevelVolumes(composeYAMLMappingValue(root, "volumes"), &risks, seenRisks)
	return risks, nil
}

func unwrapComposeYAMLNode(node *yaml.Node) *yaml.Node {
	for node != nil && node.Kind == yaml.AliasNode {
		node = node.Alias
	}
	if node != nil && node.Kind == yaml.DocumentNode && len(node.Content) == 1 {
		return unwrapComposeYAMLNode(node.Content[0])
	}
	return node
}

func composeYAMLMappingValue(node *yaml.Node, key string) *yaml.Node {
	return composeYAMLMappingValueVisited(node, key, make(map[*yaml.Node]struct{}))
}

func composeYAMLMappingValueVisited(node *yaml.Node, key string, visited map[*yaml.Node]struct{}) *yaml.Node {
	node = unwrapComposeYAMLNode(node)
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	if _, seen := visited[node]; seen {
		return nil
	}
	visited[node] = struct{}{}
	// Explicit keys override values inherited through YAML merge keys.
	for index := 0; index+1 < len(node.Content); index += 2 {
		if node.Content[index].Value == key {
			return node.Content[index+1]
		}
	}
	for index := 0; index+1 < len(node.Content); index += 2 {
		if node.Content[index].Value != "<<" {
			continue
		}
		merged := unwrapComposeYAMLNode(node.Content[index+1])
		if merged == nil {
			continue
		}
		if merged.Kind == yaml.MappingNode {
			if value := composeYAMLMappingValueVisited(merged, key, visited); value != nil {
				return value
			}
			continue
		}
		if merged.Kind == yaml.SequenceNode {
			for _, candidate := range merged.Content {
				if value := composeYAMLMappingValueVisited(candidate, key, visited); value != nil {
					return value
				}
			}
		}
	}
	return nil
}

func composeYAMLContainsKey(node *yaml.Node, key string, visited map[*yaml.Node]struct{}) bool {
	node = unwrapComposeYAMLNode(node)
	if node == nil {
		return false
	}
	if _, seen := visited[node]; seen {
		return false
	}
	visited[node] = struct{}{}
	if node.Kind == yaml.MappingNode {
		for index := 0; index+1 < len(node.Content); index += 2 {
			if node.Content[index].Value == key || composeYAMLContainsKey(node.Content[index+1], key, visited) {
				return true
			}
		}
		return false
	}
	for _, child := range node.Content {
		if composeYAMLContainsKey(child, key, visited) {
			return true
		}
	}
	return false
}

func composeYAMLBoolean(node *yaml.Node) bool {
	node = unwrapComposeYAMLNode(node)
	return node != nil && strings.EqualFold(strings.TrimSpace(node.Value), "true")
}

func composeYAMLScalar(node *yaml.Node) string {
	node = unwrapComposeYAMLNode(node)
	if node == nil || node.Kind != yaml.ScalarNode {
		return ""
	}
	return node.Value
}

func composeYAMLUsesInterpolation(node *yaml.Node) bool {
	return strings.Contains(composeYAMLScalar(node), "$")
}

func composeYAMLHasEntries(node *yaml.Node) bool {
	node = unwrapComposeYAMLNode(node)
	if node == nil {
		return false
	}
	if node.Kind == yaml.SequenceNode || node.Kind == yaml.MappingNode {
		return len(node.Content) > 0
	}
	return strings.TrimSpace(node.Value) != ""
}

func addManagedComposeRisk(risks *[]protocol.DockerComposeRisk, seen map[string]struct{}, code string, severity string, message string) {
	if _, exists := seen[code]; exists {
		return
	}
	seen[code] = struct{}{}
	*risks = append(*risks, protocol.DockerComposeRisk{Code: code, Severity: severity, Message: message})
}

func analyzeManagedComposeVolumes(node *yaml.Node, risks *[]protocol.DockerComposeRisk, seen map[string]struct{}) {
	node = unwrapComposeYAMLNode(node)
	if node == nil {
		return
	}
	if node.Kind == yaml.SequenceNode {
		for _, entry := range node.Content {
			analyzeManagedComposeVolume(entry, risks, seen)
		}
		return
	}
	analyzeManagedComposeVolume(node, risks, seen)
}

func analyzeManagedComposeTopLevelVolumes(node *yaml.Node, risks *[]protocol.DockerComposeRisk, seen map[string]struct{}) {
	node = unwrapComposeYAMLNode(node)
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}
	for index := 0; index+1 < len(node.Content); index += 2 {
		volume := unwrapComposeYAMLNode(node.Content[index+1])
		driverOptions := composeYAMLMappingValue(volume, "driver_opts")
		device := composeYAMLScalar(composeYAMLMappingValue(driverOptions, "device"))
		if device == "" {
			continue
		}
		if strings.Contains(device, "$") {
			addManagedComposeRisk(risks, seen, "absolute_bind_mount", "high", "命名卷的动态设备路径可能解析为绝对宿主机路径")
			addManagedComposeRisk(risks, seen, "docker_socket", "high", "命名卷的动态设备路径可能解析为 Docker Socket")
		}
		if strings.HasPrefix(strings.TrimSpace(device), "/") {
			addManagedComposeRisk(risks, seen, "absolute_bind_mount", "high", "命名卷使用了绝对宿主机设备路径")
		}
		if isManagedComposeDockerSocket(device) {
			addManagedComposeRisk(risks, seen, "docker_socket", "high", "命名卷指向了 Docker Socket")
		}
	}
}

func analyzeManagedComposeVolume(node *yaml.Node, risks *[]protocol.DockerComposeRisk, seen map[string]struct{}) {
	node = unwrapComposeYAMLNode(node)
	if node == nil {
		return
	}
	if node.Kind == yaml.ScalarNode {
		analyzeManagedComposeVolumeSource(node.Value, risks, seen)
		return
	}
	if node.Kind != yaml.MappingNode {
		return
	}
	source := composeYAMLScalar(composeYAMLMappingValue(node, "source"))
	if source == "" {
		source = composeYAMLScalar(composeYAMLMappingValue(node, "src"))
	}
	if source == "" {
		return
	}
	if strings.Contains(source, "$") {
		addManagedComposeRisk(risks, seen, "absolute_bind_mount", "high", "服务的动态挂载源可能解析为绝对宿主机路径")
		addManagedComposeRisk(risks, seen, "docker_socket", "high", "服务的动态挂载源可能解析为 Docker Socket")
	}
	if strings.HasPrefix(strings.TrimSpace(source), "/") {
		addManagedComposeRisk(risks, seen, "absolute_bind_mount", "high", "服务挂载了绝对宿主机路径")
	}
	if isManagedComposeDockerSocket(source) {
		addManagedComposeRisk(risks, seen, "docker_socket", "high", "服务挂载了 Docker Socket")
	}
}

func analyzeManagedComposeVolumeSource(value string, risks *[]protocol.DockerComposeRisk, seen map[string]struct{}) {
	trimmed := strings.TrimSpace(value)
	if strings.Contains(trimmed, "$") {
		addManagedComposeRisk(risks, seen, "absolute_bind_mount", "high", "服务的动态挂载源可能解析为绝对宿主机路径")
		addManagedComposeRisk(risks, seen, "docker_socket", "high", "服务的动态挂载源可能解析为 Docker Socket")
	}
	if isManagedComposeDockerSocket(trimmed) {
		addManagedComposeRisk(risks, seen, "docker_socket", "high", "服务挂载了 Docker Socket")
	}
	source := trimmed
	if separator := strings.IndexByte(source, ':'); separator >= 0 {
		source = source[:separator]
	}
	if strings.HasPrefix(strings.TrimSpace(source), "/") {
		addManagedComposeRisk(risks, seen, "absolute_bind_mount", "high", "服务挂载了绝对宿主机路径")
	}
	for _, part := range strings.Split(trimmed, ",") {
		key, value, found := strings.Cut(strings.TrimSpace(part), "=")
		if found && (key == "source" || key == "src") && strings.HasPrefix(strings.TrimSpace(value), "/") {
			addManagedComposeRisk(risks, seen, "absolute_bind_mount", "high", "服务挂载了绝对宿主机路径")
		}
	}
}

func isManagedComposeDockerSocket(value string) bool {
	lower := strings.ToLower(value)
	return strings.Contains(lower, "docker.sock") || strings.Contains(lower, "//./pipe/docker_engine")
}

var errManagedComposeProjectNotFound = errors.New("managed compose project not found")

func (h *ComposeHandler) managedComposeRootPath() string {
	if h != nil && h.managedRoot != "" {
		return h.managedRoot
	}
	return defaultManagedComposeRoot
}

func (h *ComposeHandler) ensureManagedComposeRoot(create bool) (string, error) {
	root := filepath.Clean(h.managedComposeRootPath())
	if !filepath.IsAbs(root) || strings.ContainsRune(root, '\x00') {
		return "", safeDeploymentError("托管 Compose 存储目录无效")
	}
	if create {
		if err := os.MkdirAll(root, managedComposeRootMode); err != nil {
			return "", safeDeploymentError("创建托管 Compose 存储目录失败")
		}
	}
	info, err := os.Lstat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return "", safeDeploymentError("托管 Compose 存储目录不存在")
		}
		return "", safeDeploymentError("读取托管 Compose 存储目录失败")
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", safeDeploymentError("托管 Compose 存储目录状态无效")
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil || filepath.Clean(resolved) != root {
		return "", safeDeploymentError("托管 Compose 存储目录不能使用符号链接")
	}
	if err := os.Chmod(root, managedComposeRootMode); err != nil {
		return "", safeDeploymentError("设置托管 Compose 存储目录权限失败")
	}
	return root, nil
}

func ensureManagedComposeDirectory(path string, mode os.FileMode, create bool) error {
	if create {
		if err := os.Mkdir(path, mode); err != nil && !os.IsExist(err) {
			return safeDeploymentError("创建托管 Compose 目录失败")
		}
	}
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return errManagedComposeProjectNotFound
		}
		return safeDeploymentError("读取托管 Compose 目录失败")
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return safeDeploymentError("托管 Compose 目录状态无效")
	}
	if err := os.Chmod(path, mode); err != nil {
		return safeDeploymentError("设置托管 Compose 目录权限失败")
	}
	return nil
}

func ensureManagedComposeProjectPaths(root string, projectID string, create bool) (managedComposePaths, bool, error) {
	if !validManagedComposeProjectID(projectID) {
		return managedComposePaths{}, false, safeDeploymentError("托管 Compose 项目标识无效")
	}
	projectDir := filepath.Join(root, projectID)
	relative, err := filepath.Rel(root, projectDir)
	if err != nil || relative != projectID {
		return managedComposePaths{}, false, safeDeploymentError("托管 Compose 项目路径无效")
	}
	created := false
	if create {
		if _, err := os.Lstat(projectDir); os.IsNotExist(err) {
			created = true
		}
	}
	if err := ensureManagedComposeDirectory(projectDir, managedComposeProjectMode, create); err != nil {
		return managedComposePaths{}, false, err
	}
	return managedComposePaths{
		root:         root,
		projectDir:   projectDir,
		composeFile:  filepath.Join(projectDir, "compose.yaml"),
		envFile:      filepath.Join(projectDir, ".env"),
		metadataFile: filepath.Join(projectDir, "metadata.json"),
		revisionsDir: filepath.Join(projectDir, "revisions"),
	}, created, nil
}

func ensureManagedComposeRegularFile(path string) error {
	if err := ensureManagedComposeFileParent(path); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return errManagedComposeProjectNotFound
		}
		return safeDeploymentError("读取托管 Compose 文件失败")
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return safeDeploymentError("托管 Compose 文件状态无效")
	}
	if err := os.Chmod(path, managedComposeFileMode); err != nil {
		return safeDeploymentError("设置托管 Compose 文件权限失败")
	}
	return nil
}

func ensureManagedComposeFileParent(path string) error {
	info, err := os.Lstat(filepath.Dir(path))
	if err != nil {
		return safeDeploymentError("读取托管 Compose 目录失败")
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return safeDeploymentError("托管 Compose 目录状态无效")
	}
	return nil
}

func readManagedComposeFile(path string) ([]byte, error) {
	if err := ensureManagedComposeRegularFile(path); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, safeDeploymentError("读取托管 Compose 文件失败")
	}
	return data, nil
}

func writeManagedComposeFile(path string, data []byte) error {
	if err := ensureManagedComposeFileParent(path); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return safeDeploymentError("托管 Compose 文件状态无效")
		}
	} else if !os.IsNotExist(err) {
		return safeDeploymentError("读取托管 Compose 文件失败")
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".compose-write-*")
	if err != nil {
		return safeDeploymentError("创建托管 Compose 临时文件失败")
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(managedComposeFileMode); err != nil {
		_ = temporary.Close()
		return safeDeploymentError("设置托管 Compose 文件权限失败")
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return safeDeploymentError("写入托管 Compose 文件失败")
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return safeDeploymentError("同步托管 Compose 文件失败")
	}
	if err := temporary.Close(); err != nil {
		return safeDeploymentError("关闭托管 Compose 文件失败")
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return safeDeploymentError("替换托管 Compose 文件失败")
	}
	if err := os.Chmod(path, managedComposeFileMode); err != nil {
		return safeDeploymentError("设置托管 Compose 文件权限失败")
	}
	return nil
}

func removeManagedComposeFile(path string) error {
	if err := ensureManagedComposeFileParent(path); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return safeDeploymentError("读取托管 Compose 文件失败")
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return safeDeploymentError("托管 Compose 文件状态无效")
	}
	if err := os.Remove(path); err != nil {
		return safeDeploymentError("移除托管 Compose 文件失败")
	}
	return nil
}

func (h *ComposeHandler) loadManagedComposeProject(projectID string) (managedComposeMetadata, managedComposePaths, error) {
	root, err := h.ensureManagedComposeRoot(false)
	if err != nil {
		return managedComposeMetadata{}, managedComposePaths{}, err
	}
	paths, _, err := ensureManagedComposeProjectPaths(root, projectID, false)
	if err != nil {
		return managedComposeMetadata{}, managedComposePaths{}, err
	}
	metadataData, err := readManagedComposeFile(paths.metadataFile)
	if err != nil {
		if errors.Is(err, errManagedComposeProjectNotFound) {
			return managedComposeMetadata{}, managedComposePaths{}, errManagedComposeProjectNotFound
		}
		return managedComposeMetadata{}, managedComposePaths{}, err
	}
	var metadata managedComposeMetadata
	if err := json.Unmarshal(metadataData, &metadata); err != nil {
		return managedComposeMetadata{}, managedComposePaths{}, safeDeploymentError("托管 Compose 元数据无效")
	}
	if metadata.ID != projectID || !validManagedComposeProjectID(metadata.ID) || metadata.ComposeProjectName != managedComposeProjectName(projectID) || !validManagedComposeDisplayName(metadata.DisplayName) || metadata.Revision < 1 {
		return managedComposeMetadata{}, managedComposePaths{}, safeDeploymentError("托管 Compose 元数据无效")
	}
	if err := ensureManagedComposeRegularFile(paths.composeFile); err != nil {
		return managedComposeMetadata{}, managedComposePaths{}, safeDeploymentError("托管 Compose 配置文件状态无效")
	}
	if err := ensureManagedComposeRegularFile(paths.envFile); err != nil {
		return managedComposeMetadata{}, managedComposePaths{}, safeDeploymentError("托管 Compose 环境文件状态无效")
	}
	return metadata, paths, nil
}

func (h *ComposeHandler) prepareManagedComposeProject(projectID string) (managedComposeMetadata, managedComposePaths, bool, error) {
	metadata, paths, err := h.loadManagedComposeProject(projectID)
	if err == nil {
		return metadata, paths, true, nil
	}
	if !errors.Is(err, errManagedComposeProjectNotFound) {
		return managedComposeMetadata{}, managedComposePaths{}, false, err
	}
	root, err := h.ensureManagedComposeRoot(true)
	if err != nil {
		return managedComposeMetadata{}, managedComposePaths{}, false, err
	}
	paths, created, err := ensureManagedComposeProjectPaths(root, projectID, true)
	if err != nil {
		return managedComposeMetadata{}, managedComposePaths{}, false, err
	}
	if !created {
		return managedComposeMetadata{}, managedComposePaths{}, false, safeDeploymentError("托管 Compose 项目状态无效")
	}
	return managedComposeMetadata{}, paths, false, nil
}

func writeManagedComposeMetadata(paths managedComposePaths, metadata managedComposeMetadata) error {
	data, err := json.Marshal(metadata)
	if err != nil {
		return safeDeploymentError("编码托管 Compose 元数据失败")
	}
	return writeManagedComposeFile(paths.metadataFile, data)
}

func captureManagedComposeFile(path string) (managedComposeFileBackup, error) {
	if err := ensureManagedComposeFileParent(path); err != nil {
		return managedComposeFileBackup{}, err
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return managedComposeFileBackup{}, nil
	}
	if err != nil {
		return managedComposeFileBackup{}, safeDeploymentError("读取托管 Compose 文件失败")
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return managedComposeFileBackup{}, safeDeploymentError("托管 Compose 文件状态无效")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return managedComposeFileBackup{}, safeDeploymentError("读取托管 Compose 文件失败")
	}
	return managedComposeFileBackup{exists: true, data: data}, nil
}

func (backup managedComposeFileBackup) restore(path string) error {
	if backup.exists {
		return writeManagedComposeFile(path, backup.data)
	}
	return removeManagedComposeFile(path)
}

func replaceManagedComposeFiles(paths managedComposePaths, composeYAML []byte, envFile []byte, metadata managedComposeMetadata) (func() error, error) {
	previousCompose, err := captureManagedComposeFile(paths.composeFile)
	if err != nil {
		return nil, err
	}
	previousEnv, err := captureManagedComposeFile(paths.envFile)
	if err != nil {
		return nil, err
	}
	previousMetadata, err := captureManagedComposeFile(paths.metadataFile)
	if err != nil {
		return nil, err
	}
	restore := func() error {
		var restoreErr error
		if err := previousCompose.restore(paths.composeFile); err != nil && restoreErr == nil {
			restoreErr = err
		}
		if err := previousEnv.restore(paths.envFile); err != nil && restoreErr == nil {
			restoreErr = err
		}
		if err := previousMetadata.restore(paths.metadataFile); err != nil && restoreErr == nil {
			restoreErr = err
		}
		return restoreErr
	}
	if err := writeManagedComposeFile(paths.composeFile, composeYAML); err != nil {
		return nil, err
	}
	if err := writeManagedComposeFile(paths.envFile, envFile); err != nil {
		_ = restore()
		return nil, err
	}
	if err := writeManagedComposeMetadata(paths, metadata); err != nil {
		_ = restore()
		return nil, err
	}
	return restore, nil
}

func managedComposeRevisionFileName(number int) string {
	return fmt.Sprintf("%06d-compose.yaml", number)
}

func writeManagedComposeRevision(paths managedComposePaths, number int, composeYAML []byte) (string, error) {
	if number < 1 {
		return "", safeDeploymentError("托管 Compose 版本号无效")
	}
	if err := ensureManagedComposeDirectory(paths.revisionsDir, managedComposeProjectMode, true); err != nil {
		return "", err
	}
	path := filepath.Join(paths.revisionsDir, managedComposeRevisionFileName(number))
	if err := writeManagedComposeFile(path, composeYAML); err != nil {
		return "", err
	}
	return path, nil
}

func listManagedComposeRevisions(paths managedComposePaths) ([]managedComposeRevision, error) {
	info, err := os.Lstat(paths.revisionsDir)
	if os.IsNotExist(err) {
		return []managedComposeRevision{}, nil
	}
	if err != nil {
		return nil, safeDeploymentError("读取托管 Compose 版本目录失败")
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, safeDeploymentError("托管 Compose 版本目录状态无效")
	}
	entries, err := os.ReadDir(paths.revisionsDir)
	if err != nil {
		return nil, safeDeploymentError("读取托管 Compose 版本目录失败")
	}
	revisions := make([]managedComposeRevision, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, "-compose.yaml") {
			continue
		}
		numberText := strings.TrimSuffix(name, "-compose.yaml")
		number, err := strconv.Atoi(numberText)
		if err != nil || number < 1 || managedComposeRevisionFileName(number) != name {
			continue
		}
		path := filepath.Join(paths.revisionsDir, name)
		if err := ensureManagedComposeRegularFile(path); err != nil {
			return nil, safeDeploymentError("托管 Compose 版本文件状态无效")
		}
		revisions = append(revisions, managedComposeRevision{Number: number, Path: path})
	}
	sort.Slice(revisions, func(left int, right int) bool { return revisions[left].Number < revisions[right].Number })
	return revisions, nil
}

func pruneManagedComposeRevisions(paths managedComposePaths) error {
	revisions, err := listManagedComposeRevisions(paths)
	if err != nil {
		return err
	}
	for _, revision := range revisions[:max(0, len(revisions)-managedComposeRevisionLimit)] {
		if err := removeManagedComposeFile(revision.Path); err != nil {
			return err
		}
	}
	return nil
}

func managedComposeRollbackAvailable(paths managedComposePaths, currentRevision int) bool {
	revisions, err := listManagedComposeRevisions(paths)
	if err != nil {
		return false
	}
	for _, revision := range revisions {
		if revision.Number < currentRevision {
			return true
		}
	}
	return false
}

func (h *ComposeHandler) runManagedCompose(ctx context.Context, metadata managedComposeMetadata, paths managedComposePaths, draft managedComposeDraft, action ...string) (string, error) {
	if h.runner == nil {
		return "", safeDeploymentError("Docker Compose CLI 不可用")
	}
	args := []string{"compose", "--project-name", metadata.ComposeProjectName, "--file", paths.composeFile, "--env-file", paths.envFile}
	args = append(args, action...)
	stdout, stderr, runErr := h.runner(ctx, args...)
	output := h.redactedManagedComposeOutput(stdout, stderr, draft)
	if runErr != nil {
		return output, safeDeploymentError("Docker Compose 部署失败")
	}
	return output, nil
}

func (h *ComposeHandler) restoreManagedComposeAfterFailure(ctx context.Context, previous managedComposeMetadata, paths managedComposePaths, existing bool, restore func() error, draft managedComposeDraft) error {
	if err := restore(); err != nil {
		return safeDeploymentError("Compose 部署失败，恢复先前配置失败")
	}
	if existing {
		if _, err := h.runManagedCompose(ctx, previous, paths, draft, "up", "-d"); err != nil {
			return safeDeploymentError("Compose 部署失败，恢复先前服务失败")
		}
	}
	return nil
}

func cleanupEmptyManagedComposeProject(paths managedComposePaths) {
	if info, err := os.Lstat(paths.revisionsDir); err == nil && info.Mode()&os.ModeSymlink == 0 && info.IsDir() {
		_ = os.Remove(paths.revisionsDir)
	}
	if info, err := os.Lstat(paths.projectDir); err == nil && info.Mode()&os.ModeSymlink == 0 && info.IsDir() {
		_ = os.Remove(paths.projectDir)
	}
}

func managedComposeRequestProjectID(req protocol.DockerComposeDeploymentRequest) (string, error) {
	projectID := strings.TrimSpace(req.ProjectID)
	if !validManagedComposeProjectID(projectID) {
		return "", safeDeploymentError("托管 Compose 项目标识无效")
	}
	return projectID, nil
}

func (h *ComposeHandler) handleManagedComposeRollback(ctx context.Context, req protocol.DockerComposeDeploymentRequest, response protocol.DockerComposeDeploymentResponse) protocol.DockerComposeDeploymentResponse {
	projectID, err := managedComposeRequestProjectID(req)
	if err != nil {
		response.Error = deploymentResponseError(err)
		return response
	}
	operationKey := managedComposeProjectName(projectID)
	if !h.beginComposeOperation(operationKey) {
		response.Error = "该 Compose 项目已有操作正在执行"
		return response
	}
	defer h.finishComposeOperation(operationKey)

	metadata, paths, err := h.loadManagedComposeProject(projectID)
	if err != nil {
		response.Error = "托管 Compose 项目不存在或状态无效"
		return response
	}
	revisions, err := listManagedComposeRevisions(paths)
	if err != nil {
		response.Error = deploymentResponseError(err)
		return response
	}
	var previous *managedComposeRevision
	for index := range revisions {
		if revisions[index].Number < metadata.Revision {
			candidate := revisions[index]
			previous = &candidate
		}
	}
	if previous == nil {
		response.Error = "没有可回滚的 Compose 版本"
		return response
	}
	previousYAML, err := readManagedComposeFile(previous.Path)
	if err != nil {
		response.Error = deploymentResponseError(err)
		return response
	}
	envFile, err := readManagedComposeFile(paths.envFile)
	if err != nil {
		response.Error = deploymentResponseError(err)
		return response
	}
	draft := managedComposeDraft{
		ProjectID:   projectID,
		DisplayName: metadata.DisplayName,
		ComposeYAML: string(previousYAML),
		EnvFile:     string(envFile),
	}
	risks, output, err := h.validateManagedComposeDraft(ctx, draft)
	response.Risks = risks
	response.Output = output
	if err != nil {
		response.Error = deploymentResponseError(err)
		return response
	}

	nextMetadata := metadata
	nextMetadata.Revision++
	nextMetadata.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	revisionPath, err := writeManagedComposeRevision(paths, nextMetadata.Revision, previousYAML)
	if err != nil {
		response.Error = deploymentResponseError(err)
		return response
	}
	restore, err := replaceManagedComposeFiles(paths, previousYAML, envFile, nextMetadata)
	if err != nil {
		_ = removeManagedComposeFile(revisionPath)
		response.Error = deploymentResponseError(err)
		return response
	}
	output, runErr := h.runManagedCompose(ctx, nextMetadata, paths, draft, "up", "-d")
	if output != "" {
		response.Output = boundedComposeOutput(strings.TrimSpace(response.Output+"\n"+output), "")
	}
	if runErr != nil {
		restoreErr := restore()
		if restoreErr == nil {
			_, restoreErr = h.runManagedCompose(ctx, metadata, paths, draft, "up", "-d")
		}
		_ = removeManagedComposeFile(revisionPath)
		if restoreErr != nil {
			response.Error = "Compose 回滚失败，恢复先前配置失败"
		} else {
			response.Error = deploymentResponseError(runErr)
		}
		return response
	}
	if err := pruneManagedComposeRevisions(paths); err != nil {
		response.Error = deploymentResponseError(err)
		return response
	}
	response.Project = managedComposeProject(nextMetadata, managedComposeRollbackAvailable(paths, nextMetadata.Revision))
	response.Success = true
	return response
}

func (h *ComposeHandler) handleManagedComposeArchive(ctx context.Context, req protocol.DockerComposeDeploymentRequest, response protocol.DockerComposeDeploymentResponse) protocol.DockerComposeDeploymentResponse {
	projectID, err := managedComposeRequestProjectID(req)
	if err != nil {
		response.Error = deploymentResponseError(err)
		return response
	}
	operationKey := managedComposeProjectName(projectID)
	if !h.beginComposeOperation(operationKey) {
		response.Error = "该 Compose 项目已有操作正在执行"
		return response
	}
	defer h.finishComposeOperation(operationKey)

	metadata, paths, err := h.loadManagedComposeProject(projectID)
	if err != nil {
		response.Error = "托管 Compose 项目不存在或状态无效"
		return response
	}
	envFile, err := readManagedComposeFile(paths.envFile)
	if err != nil {
		response.Error = deploymentResponseError(err)
		return response
	}
	draft := managedComposeDraft{ProjectID: projectID, DisplayName: metadata.DisplayName, EnvFile: string(envFile)}
	output, runErr := h.runManagedCompose(ctx, metadata, paths, draft, "down")
	response.Output = output
	if runErr != nil {
		response.Error = deploymentResponseError(runErr)
		return response
	}

	archiveRoot := filepath.Join(paths.root, "archive")
	relative, relErr := filepath.Rel(paths.root, archiveRoot)
	if relErr != nil || relative != "archive" {
		response.Error = "托管 Compose 归档路径无效"
		return response
	}
	if err := ensureManagedComposeDirectory(archiveRoot, managedComposeRootMode, true); err != nil {
		response.Error = deploymentResponseError(err)
		return response
	}
	archiveName := projectID + "-" + time.Now().UTC().Format("20060102T150405.000000000Z")
	archivePath := filepath.Join(archiveRoot, archiveName)
	if relative, err := filepath.Rel(archiveRoot, archivePath); err != nil || relative != archiveName {
		response.Error = "托管 Compose 归档路径无效"
		return response
	}
	if _, err := os.Lstat(archivePath); err == nil {
		response.Error = "托管 Compose 归档目标已存在"
		return response
	} else if !os.IsNotExist(err) {
		response.Error = "读取托管 Compose 归档目录失败"
		return response
	}
	if _, _, err := ensureManagedComposeProjectPaths(paths.root, projectID, false); err != nil {
		response.Error = "托管 Compose 项目状态无效"
		return response
	}
	if err := os.Rename(paths.projectDir, archivePath); err != nil {
		response.Error = "归档托管 Compose 项目失败"
		return response
	}
	if err := os.Chmod(archivePath, managedComposeProjectMode); err != nil {
		response.Error = "设置托管 Compose 归档权限失败"
		return response
	}
	project := managedComposeProject(metadata, false)
	project.Status = "archived"
	response.Project = project
	response.Success = true
	return response
}

type managedComposeProjectDescriptor struct {
	metadata          managedComposeMetadata
	paths             managedComposePaths
	rollbackAvailable bool
}

func (h *ComposeHandler) managedComposeProjectDescriptors() map[string]managedComposeProjectDescriptor {
	root, err := h.ensureManagedComposeRoot(false)
	if err != nil {
		return nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	descriptors := make(map[string]managedComposeProjectDescriptor)
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() || !validManagedComposeProjectID(entry.Name()) {
			continue
		}
		metadata, paths, err := h.loadManagedComposeProject(entry.Name())
		if err != nil {
			continue
		}
		descriptors[metadata.ComposeProjectName] = managedComposeProjectDescriptor{
			metadata:          metadata,
			paths:             paths,
			rollbackAvailable: managedComposeRollbackAvailable(paths, metadata.Revision),
		}
	}
	return descriptors
}

// mergeManagedComposeProjects augments Docker's live/cache result with
// Agent-owned project metadata. The internal ConfigFiles value remains only
// long enough for the legacy action handler; HandleDockerComposeList scrubs
// it before serializing the response.
func (h *ComposeHandler) mergeManagedComposeProjects(projects []protocol.DockerComposeProject) []protocol.DockerComposeProject {
	descriptors := h.managedComposeProjectDescriptors()
	if len(descriptors) == 0 {
		for index := range projects {
			if projects[index].Management == "" {
				projects[index].Management = "external"
			}
		}
		return projects
	}

	merged := make([]protocol.DockerComposeProject, 0, len(projects)+len(descriptors))
	seen := make(map[string]struct{}, len(projects))
	for _, project := range projects {
		descriptor, managed := descriptors[project.Name]
		if !managed {
			if project.Management == "" {
				project.Management = "external"
			}
			merged = append(merged, project)
			continue
		}
		managedProject := managedComposeProject(descriptor.metadata, descriptor.rollbackAvailable)
		managedProject.Status = project.Status
		managedProject.Services = project.Services
		managedProject.Error = project.Error
		managedProject.ConfigFiles = []string{descriptor.paths.composeFile}
		merged = append(merged, managedProject)
		seen[project.Name] = struct{}{}
	}

	missing := make([]string, 0, len(descriptors))
	for projectName := range descriptors {
		if _, exists := seen[projectName]; !exists {
			missing = append(missing, projectName)
		}
	}
	sort.Strings(missing)
	for _, projectName := range missing {
		descriptor := descriptors[projectName]
		managedProject := managedComposeProject(descriptor.metadata, descriptor.rollbackAvailable)
		managedProject.Status = "stopped"
		managedProject.ConfigFiles = []string{descriptor.paths.composeFile}
		merged = append(merged, managedProject)
	}
	return merged
}
