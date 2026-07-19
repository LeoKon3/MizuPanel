package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mizupanel/mizupanel/internal/protocol"
)

const (
	composeListTimeout      = 45 * time.Second
	composeActionTimeout    = 5 * time.Minute
	composeOutputLimit      = 32 * 1024
	defaultComposeCachePath = "/usr/local/mizupanel/var/compose-projects.json"
)

var composeActions = map[string][]string{
	"pull":     {"pull"},
	"up":       {"up", "-d"},
	"restart":  {"restart"},
	"stop":     {"stop"},
	"down":     {"down"},
	"logs":     {"logs", "--no-color", "--tail", "200"},
	"validate": {"config", "--quiet"},
}

var composeServiceActions = map[string]bool{
	"pull":    true,
	"up":      true,
	"restart": true,
	"stop":    true,
}

type composeCommandRunner func(context.Context, ...string) (stdout string, stderr string, err error)

// ComposeHandler exposes a deliberately small, structured Docker Compose API.
// Project paths are taken from the Agent's own discovery result; callers never
// provide a shell command or an arbitrary compose file path.
type ComposeHandler struct {
	supported bool
	runner    composeCommandRunner
	cachePath string
	mu        sync.Mutex
	active    map[string]bool
	cacheMu   sync.Mutex
}

func NewComposeHandler() *ComposeHandler {
	_, err := exec.LookPath("docker")
	handler := &ComposeHandler{supported: err == nil, runner: runComposeCommand, cachePath: defaultComposeCachePath, active: make(map[string]bool)}
	if !handler.supported {
		return handler
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _, err = handler.runner(ctx, "compose", "version", "--short")
	handler.supported = err == nil
	return handler
}

func (h *ComposeHandler) SupportsServiceActions() bool {
	return h != nil && h.supported
}

func (h *ComposeHandler) HandleDockerComposeList(ctx context.Context, req protocol.DockerComposeListRequest) protocol.DockerComposeListResponse {
	response := protocol.DockerComposeListResponse{Type: protocol.MessageTypeDockerComposeListResponse, RequestID: req.RequestID, Supported: h.supported, ServiceActionsSupported: h.SupportsServiceActions(), Projects: []protocol.DockerComposeProject{}}
	if !h.supported {
		response.Error = "Docker Compose CLI 不可用"
		return response
	}
	ctx, cancel := context.WithTimeout(ctx, composeListTimeout)
	defer cancel()
	projects, err := h.discover(ctx)
	if err != nil {
		response.Error = err.Error()
		return response
	}
	response.Success = true
	response.Projects = projects
	return response
}

func (h *ComposeHandler) HandleDockerComposeAction(ctx context.Context, req protocol.DockerComposeActionRequest) protocol.DockerComposeActionResponse {
	response := protocol.DockerComposeActionResponse{Type: protocol.MessageTypeDockerComposeActionResponse, RequestID: req.RequestID, ProjectName: strings.TrimSpace(req.ProjectName), ServiceName: strings.TrimSpace(req.ServiceName), Action: strings.TrimSpace(req.Action)}
	actionArgs, ok := composeActions[response.Action]
	if !ok {
		response.Error = "不支持的 Compose 操作"
		return response
	}
	if !validComposeProjectName(response.ProjectName) {
		response.Error = "Compose 项目标识无效"
		return response
	}
	if response.ServiceName != "" {
		if !composeServiceActions[response.Action] {
			response.Error = "该 Compose 操作不支持服务范围"
			return response
		}
		if !validComposeServiceName(response.ServiceName) {
			response.Error = "Compose 服务标识无效"
			return response
		}
	}
	if !h.supported {
		response.Error = "Docker Compose CLI 不可用"
		return response
	}

	h.mu.Lock()
	if h.active[response.ProjectName] {
		h.mu.Unlock()
		response.Error = "该 Compose 项目已有操作正在执行"
		return response
	}
	h.active[response.ProjectName] = true
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.active, response.ProjectName)
		h.mu.Unlock()
	}()

	ctx, cancel := context.WithTimeout(ctx, composeActionTimeout)
	defer cancel()
	projects, err := h.discover(ctx)
	if err != nil {
		response.Error = err.Error()
		return response
	}
	var project *protocol.DockerComposeProject
	for index := range projects {
		if projects[index].Name == response.ProjectName {
			project = &projects[index]
			break
		}
	}
	if project == nil {
		response.Error = "Compose 项目不存在或尚未被 Docker Compose 发现"
		return response
	}
	if response.ServiceName != "" && !composeProjectHasService(*project, response.ServiceName) {
		response.Error = "Compose 服务不存在或当前没有可操作的容器"
		return response
	}
	args := composeProjectArgs(*project)
	args = append(args, actionArgs...)
	if response.ServiceName != "" {
		args = append(args, response.ServiceName)
	}
	stdout, stderr, runErr := h.runner(ctx, args...)
	if response.Action == "validate" {
		stdout = sanitizeComposeValidationOutput(stdout)
		stderr = sanitizeComposeValidationOutput(stderr)
	}
	response.Output = boundedComposeOutput(stdout, stderr)
	if runErr != nil {
		response.Error = composeCommandError("执行 Compose 操作失败", stderr, runErr).Error()
		return response
	}
	response.Success = true
	return response
}

type composeProjectRecord struct {
	Name        string          `json:"Name"`
	Status      string          `json:"Status"`
	ConfigFiles json.RawMessage `json:"ConfigFiles"`
}

type composeServiceRecord struct {
	ID         string `json:"ID"`
	Name       string `json:"Name"`
	Service    string `json:"Service"`
	Image      string `json:"Image"`
	State      string `json:"State"`
	Status     string `json:"Status"`
	Health     string `json:"Health"`
	Publishers []struct {
		PublishedPort uint16 `json:"PublishedPort"`
		TargetPort    uint16 `json:"TargetPort"`
		Protocol      string `json:"Protocol"`
	} `json:"Publishers"`
}

type composeProjectCache struct {
	Projects []composeProjectCacheRecord `json:"projects"`
}

type composeProjectCacheRecord struct {
	Name        string   `json:"name"`
	ConfigFiles []string `json:"config_files"`
}

func (h *ComposeHandler) discover(ctx context.Context) ([]protocol.DockerComposeProject, error) {
	stdout, stderr, err := h.runner(ctx, "compose", "ls", "--all", "--format", "json")
	if err != nil {
		return nil, composeCommandError("发现 Compose 项目失败", stderr, err)
	}
	records, err := decodeComposeRecords[composeProjectRecord](stdout)
	if err != nil {
		return nil, fmt.Errorf("解析 Compose 项目失败: %w", err)
	}
	liveProjects := make([]protocol.DockerComposeProject, 0, len(records))
	for _, record := range records {
		if !validComposeProjectName(record.Name) {
			continue
		}
		project := protocol.DockerComposeProject{Name: record.Name, Status: record.Status, ConfigFiles: parseComposeFiles(record.ConfigFiles), Services: []protocol.DockerComposeService{}}
		liveProjects = append(liveProjects, project)
	}
	projects := h.mergeCachedProjects(liveProjects)
	for index := range projects {
		project := &projects[index]
		if len(project.ConfigFiles) == 0 {
			project.Error = "Docker 未返回 Compose 配置文件路径"
			continue
		}
		args := composeProjectArgs(*project)
		args = append(args, "ps", "--all", "--format", "json")
		psOutput, psStderr, psErr := h.runner(ctx, args...)
		if psErr != nil {
			project.Error = composeCommandError("读取 Compose 服务失败", psStderr, psErr).Error()
			continue
		}
		services, parseErr := decodeComposeRecords[composeServiceRecord](psOutput)
		if parseErr != nil {
			project.Error = fmt.Sprintf("解析 Compose 服务失败: %v", parseErr)
		} else {
			project.Services = composeServices(services)
		}
	}
	return projects, nil
}

// mergeCachedProjects keeps projects discoverable after `docker compose down`.
// The cache is only populated from docker compose ls output and is never
// treated as permission to read an arbitrary path supplied by a caller.
func (h *ComposeHandler) mergeCachedProjects(live []protocol.DockerComposeProject) []protocol.DockerComposeProject {
	if h.cachePath == "" {
		return live
	}
	h.cacheMu.Lock()
	defer h.cacheMu.Unlock()

	cached, err := h.readComposeProjectCache()
	if err != nil {
		cached = nil
	}
	merged := make([]protocol.DockerComposeProject, 0, len(live)+len(cached))
	cachedByName := make(map[string]protocol.DockerComposeProject, len(cached))
	for _, project := range cached {
		if validCachedComposeProject(project) {
			cachedByName[project.Name] = project
		}
	}
	seen := make(map[string]struct{}, len(live)+len(cached))
	for _, project := range live {
		if _, exists := seen[project.Name]; exists {
			continue
		}
		if len(project.ConfigFiles) == 0 {
			if cachedProject, exists := cachedByName[project.Name]; exists {
				project.ConfigFiles = cachedProject.ConfigFiles
			}
		}
		seen[project.Name] = struct{}{}
		merged = append(merged, project)
	}
	for _, project := range cached {
		if _, exists := seen[project.Name]; exists || !validCachedComposeProject(project) {
			continue
		}
		seen[project.Name] = struct{}{}
		merged = append(merged, project)
	}
	_ = h.writeComposeProjectCache(merged)
	return merged
}

func (h *ComposeHandler) readComposeProjectCache() ([]protocol.DockerComposeProject, error) {
	data, err := os.ReadFile(h.cachePath)
	if err != nil {
		return nil, err
	}
	var cache composeProjectCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, err
	}
	projects := make([]protocol.DockerComposeProject, 0, len(cache.Projects))
	for _, record := range cache.Projects {
		if !validComposeProjectName(record.Name) {
			continue
		}
		projects = append(projects, protocol.DockerComposeProject{Name: record.Name, ConfigFiles: cleanComposeFiles(record.ConfigFiles), Services: []protocol.DockerComposeService{}})
	}
	return projects, nil
}

func validCachedComposeProject(project protocol.DockerComposeProject) bool {
	if !validComposeProjectName(project.Name) || len(project.ConfigFiles) == 0 {
		return false
	}
	for _, configFile := range project.ConfigFiles {
		info, err := os.Stat(configFile)
		if err != nil || !info.Mode().IsRegular() {
			return false
		}
	}
	return true
}

func (h *ComposeHandler) writeComposeProjectCache(projects []protocol.DockerComposeProject) error {
	if h.cachePath == "" {
		return nil
	}
	cache := composeProjectCache{Projects: make([]composeProjectCacheRecord, 0, len(projects))}
	seen := make(map[string]struct{}, len(projects))
	for _, project := range projects {
		if _, exists := seen[project.Name]; exists || !validComposeProjectName(project.Name) || len(project.ConfigFiles) == 0 {
			continue
		}
		files := cleanComposeFiles(project.ConfigFiles)
		if len(files) == 0 {
			continue
		}
		seen[project.Name] = struct{}{}
		cache.Projects = append(cache.Projects, composeProjectCacheRecord{Name: project.Name, ConfigFiles: files})
	}
	if err := os.MkdirAll(filepath.Dir(h.cachePath), 0750); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(h.cachePath), ".compose-projects-*.tmp")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(0600); err != nil {
		_ = temp.Close()
		return err
	}
	encoder := json.NewEncoder(temp)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(cache); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempName, h.cachePath)
}

func composeServices(records []composeServiceRecord) []protocol.DockerComposeService {
	services := make([]protocol.DockerComposeService, 0, len(records))
	for _, record := range records {
		name := record.Service
		if name == "" {
			name = record.Name
		}
		ports := make([]string, 0, len(record.Publishers))
		for _, publisher := range record.Publishers {
			if publisher.PublishedPort == 0 {
				continue
			}
			ports = append(ports, fmt.Sprintf("%d:%d/%s", publisher.PublishedPort, publisher.TargetPort, strings.ToLower(publisher.Protocol)))
		}
		services = append(services, protocol.DockerComposeService{Name: name, ContainerName: record.Name, ContainerID: record.ID, Image: record.Image, State: record.State, Status: record.Status, Health: record.Health, Ports: ports})
	}
	return services
}

func composeProjectHasService(project protocol.DockerComposeProject, serviceName string) bool {
	for _, service := range project.Services {
		if service.Name == serviceName {
			return true
		}
	}
	return false
}

func composeProjectArgs(project protocol.DockerComposeProject) []string {
	args := []string{"compose", "--project-name", project.Name}
	for _, file := range project.ConfigFiles {
		args = append(args, "--file", file)
	}
	return args
}

func parseComposeFiles(raw json.RawMessage) []string {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var files []string
	if json.Unmarshal(raw, &files) == nil {
		return cleanComposeFiles(files)
	}
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return cleanComposeFiles(strings.Split(value, ","))
	}
	return nil
}

func cleanComposeFiles(files []string) []string {
	cleaned := make([]string, 0, len(files))
	for _, file := range files {
		file = strings.TrimSpace(file)
		if file == "" || !filepath.IsAbs(file) || strings.ContainsRune(file, '\x00') {
			continue
		}
		cleaned = append(cleaned, filepath.Clean(file))
	}
	return cleaned
}

func validComposeProjectName(name string) bool {
	if name == "" || len(name) > 128 {
		return false
	}
	for _, char := range name {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '_' || char == '-' {
			continue
		}
		return false
	}
	return true
}

func validComposeServiceName(name string) bool {
	if name == "" || len(name) > 128 {
		return false
	}
	for _, char := range name {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '_' || char == '-' || char == '.' {
			continue
		}
		return false
	}
	return true
}

func decodeComposeRecords[T any](output string) ([]T, error) {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return []T{}, nil
	}
	var records []T
	if json.Unmarshal([]byte(trimmed), &records) == nil {
		return records, nil
	}
	lines := strings.Split(trimmed, "\n")
	records = make([]T, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var record T
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

func runComposeCommand(ctx context.Context, args ...string) (string, string, error) {
	command := exec.CommandContext(ctx, "docker", args...)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	return stdout.String(), stderr.String(), err
}

func composeCommandError(prefix string, stderr string, err error) error {
	detail := strings.TrimSpace(stderr)
	if detail == "" {
		detail = err.Error()
	}
	return fmt.Errorf("%s: %s", prefix, boundedComposeOutput(detail, ""))
}

func boundedComposeOutput(stdout string, stderr string) string {
	output := strings.TrimSpace(stdout)
	if strings.TrimSpace(stderr) != "" {
		if output != "" {
			output += "\n"
		}
		output += strings.TrimSpace(stderr)
	}
	if len(output) > composeOutputLimit {
		return output[:composeOutputLimit] + "\n…输出已截断"
	}
	return output
}

func sanitizeComposeValidationOutput(output string) string {
	sensitiveKeys := []string{"password", "passwd", "secret", "token", "api_key", "apikey", "access_key", "private_key"}
	lines := strings.Split(output, "\n")
	for index, line := range lines {
		lower := strings.ToLower(line)
		sensitiveAt := -1
		for _, key := range sensitiveKeys {
			if found := strings.Index(lower, key); found >= 0 && (sensitiveAt == -1 || found < sensitiveAt) {
				sensitiveAt = found
			}
		}
		if sensitiveAt == -1 {
			continue
		}
		separatorAt := strings.IndexAny(line[sensitiveAt:], ":=")
		if separatorAt == -1 {
			lines[index] = "[包含敏感字段的错误详情已隐藏]"
			continue
		}
		separatorAt += sensitiveAt
		lines[index] = line[:separatorAt+1] + " [敏感内容已隐藏]"
	}
	return strings.Join(lines, "\n")
}
