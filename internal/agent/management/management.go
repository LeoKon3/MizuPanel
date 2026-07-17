package management

import (
	"context"
	"os/exec"
	"runtime"
	"strconv"
	"time"

	"github.com/mizupanel/mizupanel/internal/version"

	"github.com/mizupanel/mizupanel/internal/protocol"
)

const (
	ServiceName       = "mizupanel-agent"
	MaxLogOutputBytes = 128 * 1024
)

type Runner interface {
	Run(context.Context, string, ...string) error
	Output(context.Context, string, ...string) (string, error)
}

type Options struct {
	Version         string
	User            string
	Mode            string
	TerminalEnabled bool
	DockerAvailable bool
	DockerError     string
	DockerStatus    func() (bool, string)
	ConfigPath      string
	StartTime       time.Time
	GOOS            string
	Runner          Runner
	Upgrade         func(protocol.AgentUpgradeRequest, func(protocol.AgentUpgradeResponse)) protocol.AgentUpgradeResponse
}

type Handler struct {
	options Options
}

func (h *Handler) Upgrade(request protocol.AgentUpgradeRequest, report func(protocol.AgentUpgradeResponse)) protocol.AgentUpgradeResponse {
	if h.options.Upgrade == nil {
		return protocol.AgentUpgradeResponse{Code: "unsupported", Error: "当前 Agent 暂不支持一键升级。"}
	}
	return h.options.Upgrade(request, report)
}

type commandRunner struct{}

func NewHandler(options Options) *Handler {
	if options.Version == "" {
		options.Version = version.Current
	}
	if options.Mode == "" {
		options.Mode = "normal"
	}
	if options.StartTime.IsZero() {
		options.StartTime = time.Now()
	}
	if options.GOOS == "" {
		options.GOOS = runtime.GOOS
	}
	if options.Runner == nil {
		options.Runner = commandRunner{}
	}
	return &Handler{options: options}
}

func (h *Handler) Status() protocol.AgentStatusResponse {
	now := time.Now()
	uptime := int64(now.Sub(h.options.StartTime).Seconds())
	if uptime < 0 {
		uptime = 0
	}
	dockerAvailable := h.options.DockerAvailable
	dockerError := h.options.DockerError
	if h.options.DockerStatus != nil {
		dockerAvailable, dockerError = h.options.DockerStatus()
	}
	return protocol.AgentStatusResponse{
		Version:         h.options.Version,
		User:            h.options.User,
		Mode:            h.options.Mode,
		TerminalEnabled: h.options.TerminalEnabled,
		DockerAvailable: dockerAvailable,
		DockerError:     dockerError,
		ConfigPath:      h.options.ConfigPath,
		ServiceName:     ServiceName,
		Uptime:          uptime,
		CollectedAt:     now.Unix(),
	}
}

func (h *Handler) Restart() protocol.AgentRestartResponse {
	if h.options.GOOS != "linux" {
		return protocol.AgentRestartResponse{Code: "unsupported", Error: "当前版本暂不支持非 Linux Agent 管理。"}
	}
	go func() {
		_ = h.options.Runner.Run(context.Background(), "systemctl", "restart", ServiceName)
	}()
	return protocol.AgentRestartResponse{Accepted: true, Message: "重启命令已下发，等待 Agent 重新连接"}
}

func (h *Handler) Logs(lines int) protocol.AgentLogsResponse {
	if h.options.GOOS != "linux" {
		return protocol.AgentLogsResponse{Code: "unsupported", Error: "当前版本暂不支持非 Linux Agent 管理。"}
	}
	lines = clampLines(lines)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	content, err := h.options.Runner.Output(ctx, "journalctl", "-u", ServiceName, "-n", strconv.Itoa(lines), "--no-pager")
	response := protocol.AgentLogsResponse{Lines: lines, CollectedAt: time.Now().Unix()}
	if err != nil {
		response.Code = "failed"
		response.Error = err.Error()
		response.Content = truncate(content)
		response.Truncated = len(content) > MaxLogOutputBytes
		return response
	}
	response.Content = truncate(content)
	response.Truncated = len(content) > MaxLogOutputBytes
	return response
}

func clampLines(lines int) int {
	if lines < 1 {
		return 1
	}
	if lines > 500 {
		return 500
	}
	return lines
}

func truncate(content string) string {
	if len(content) <= MaxLogOutputBytes {
		return content
	}
	return content[:MaxLogOutputBytes]
}

func (commandRunner) Run(ctx context.Context, name string, args ...string) error {
	return exec.CommandContext(ctx, name, args...).Run()
}

func (commandRunner) Output(ctx context.Context, name string, args ...string) (string, error) {
	output, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	return string(output), err
}
