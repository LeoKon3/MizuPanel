package systemd

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/mizupanel/mizupanel/internal/protocol"
)

const (
	listTimeout       = 20 * time.Second
	actionTimeout     = 60 * time.Second
	logTimeout        = 20 * time.Second
	logLineLimit      = 200
	outputLimit       = 64 * 1024
	excludedAgentUnit = "mizupanel-agent.service"
)

var allowedActions = map[string]bool{
	"start":   true,
	"stop":    true,
	"restart": true,
	"logs":    true,
}

type commandRunner func(context.Context, string, ...string) (stdout string, stderr string, err error)

// Handler exposes a small, structured systemd API. It never accepts a full
// command or a caller supplied argument list.
type Handler struct {
	supported bool
	runner    commandRunner
	mu        sync.Mutex
	active    map[string]bool
}

func NewHandler() *Handler {
	handler := &Handler{runner: runCommand, active: make(map[string]bool)}
	if runtime.GOOS != "linux" {
		return handler
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		return handler
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, _, err := handler.runner(ctx, "systemctl", "show", "--property=Version")
	handler.supported = err == nil
	return handler
}

func (h *Handler) Supported() bool {
	return h != nil && h.supported
}

func (h *Handler) HandleList(ctx context.Context, req protocol.SystemdServiceListRequest) protocol.SystemdServiceListResponse {
	response := protocol.SystemdServiceListResponse{
		Type:      protocol.MessageTypeSystemdServiceListResponse,
		RequestID: req.RequestID,
		Supported: h.Supported(),
		Services:  []protocol.SystemdService{},
	}
	if !response.Supported {
		response.Error = "systemd 不可用"
		return response
	}
	ctx, cancel := context.WithTimeout(ctx, listTimeout)
	defer cancel()
	services, err := h.list(ctx)
	if err != nil {
		response.Error = err.Error()
		return response
	}
	response.Success = true
	response.Services = services
	return response
}

func (h *Handler) HandleAction(ctx context.Context, req protocol.SystemdServiceActionRequest) protocol.SystemdServiceActionResponse {
	response := protocol.SystemdServiceActionResponse{
		Type:        protocol.MessageTypeSystemdServiceActionResponse,
		RequestID:   req.RequestID,
		ServiceName: strings.TrimSpace(req.ServiceName),
		Action:      strings.TrimSpace(req.Action),
	}
	if !allowedActions[response.Action] {
		response.Error = "不支持的 systemd 服务操作"
		return response
	}
	if !validServiceName(response.ServiceName) || response.ServiceName == excludedAgentUnit {
		response.Error = "systemd 服务标识无效或不可通过此入口操作"
		return response
	}
	if !h.Supported() {
		response.Error = "systemd 不可用"
		return response
	}

	h.mu.Lock()
	if h.active[response.ServiceName] {
		h.mu.Unlock()
		response.Error = "该 systemd 服务已有操作正在执行"
		return response
	}
	h.active[response.ServiceName] = true
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.active, response.ServiceName)
		h.mu.Unlock()
	}()

	timeout := actionTimeout
	if response.Action == "logs" {
		timeout = logTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	services, err := h.list(ctx)
	if err != nil {
		response.Error = err.Error()
		return response
	}
	if !containsService(services, response.ServiceName) {
		response.Error = "systemd 服务不存在或当前不可操作"
		return response
	}

	if response.Action == "logs" {
		stdout, stderr, runErr := h.runner(ctx, "journalctl", "--unit", response.ServiceName, "--no-pager", "--output=short-iso", "--lines", fmt.Sprintf("%d", logLineLimit))
		response.Output = boundedOutput(sanitizeLogOutput(stdout), sanitizeLogOutput(stderr))
		if runErr != nil {
			response.Error = commandError("读取 systemd 服务日志失败", stderr, runErr).Error()
			return response
		}
		response.Success = true
		return response
	}

	stdout, stderr, runErr := h.runner(ctx, "systemctl", response.Action, response.ServiceName)
	response.Output = boundedOutput(stdout, stderr)
	if runErr != nil {
		response.Error = commandError("执行 systemd 服务操作失败", stderr, runErr).Error()
		return response
	}
	response.Success = true
	return response
}

func (h *Handler) list(ctx context.Context) ([]protocol.SystemdService, error) {
	unitsOutput, unitsStderr, err := h.runner(ctx, "systemctl", "list-units", "--type=service", "--all", "--no-legend", "--plain", "--no-pager")
	if err != nil {
		return nil, commandError("读取 systemd 服务失败", unitsStderr, err)
	}
	filesOutput, _, _ := h.runner(ctx, "systemctl", "list-unit-files", "--type=service", "--no-legend", "--plain", "--no-pager")
	states := parseUnitFileStates(filesOutput)
	return parseServices(unitsOutput, states), nil
}

func parseServices(output string, unitFileStates map[string]string) []protocol.SystemdService {
	services := make([]protocol.SystemdService, 0)
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 4 {
			continue
		}
		if fields[0] == "●" {
			fields = fields[1:]
		}
		if len(fields) < 4 || !validServiceName(fields[0]) || fields[0] == excludedAgentUnit {
			continue
		}
		services = append(services, protocol.SystemdService{
			Name:          fields[0],
			LoadState:     fields[1],
			ActiveState:   fields[2],
			SubState:      fields[3],
			Description:   strings.Join(fields[4:], " "),
			UnitFileState: unitFileStates[fields[0]],
		})
	}
	return services
}

func parseUnitFileStates(output string) map[string]string {
	states := make(map[string]string)
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 || !validServiceName(fields[0]) || fields[0] == excludedAgentUnit {
			continue
		}
		states[fields[0]] = fields[1]
	}
	return states
}

func containsService(services []protocol.SystemdService, name string) bool {
	for _, service := range services {
		if service.Name == name {
			return true
		}
	}
	return false
}

func validServiceName(name string) bool {
	if len(name) < len("x.service") || len(name) > 255 || !strings.HasSuffix(name, ".service") {
		return false
	}
	for _, char := range name {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '@' || char == '_' || char == '-' || char == '.' {
			continue
		}
		return false
	}
	return true
}

func runCommand(ctx context.Context, name string, args ...string) (string, string, error) {
	command := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	return stdout.String(), stderr.String(), err
}

func commandError(prefix string, stderr string, err error) error {
	detail := strings.TrimSpace(sanitizeLogOutput(stderr))
	if detail == "" {
		detail = err.Error()
	}
	return fmt.Errorf("%s: %s", prefix, boundedOutput(detail, ""))
}

func boundedOutput(stdout string, stderr string) string {
	output := strings.TrimSpace(stdout)
	if strings.TrimSpace(stderr) != "" {
		if output != "" {
			output += "\n"
		}
		output += strings.TrimSpace(stderr)
	}
	if len(output) > outputLimit {
		return output[:outputLimit] + "\n…输出已截断"
	}
	return output
}

func sanitizeLogOutput(output string) string {
	keys := []string{"password", "passwd", "secret", "token", "api_key", "apikey", "access_key", "private_key", "authorization"}
	lines := strings.Split(output, "\n")
	for index, line := range lines {
		lower := strings.ToLower(line)
		matchedAt := -1
		for _, key := range keys {
			if found := strings.Index(lower, key); found >= 0 && (matchedAt == -1 || found < matchedAt) {
				matchedAt = found
			}
		}
		if matchedAt == -1 {
			continue
		}
		separatorAt := strings.IndexAny(line[matchedAt:], ":=")
		if separatorAt == -1 {
			lines[index] = "[包含敏感字段的日志内容已隐藏]"
			continue
		}
		separatorAt += matchedAt
		lines[index] = line[:separatorAt+1] + " [敏感内容已隐藏]"
	}
	return strings.Join(lines, "\n")
}
