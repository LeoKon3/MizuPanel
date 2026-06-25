package app

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mizupanel/mizupanel/internal/server/agenthub"
	"github.com/mizupanel/mizupanel/internal/server/alerting"
	"github.com/mizupanel/mizupanel/internal/server/api"
	"github.com/mizupanel/mizupanel/internal/server/k8s"
	"github.com/mizupanel/mizupanel/internal/server/sshops"
	"github.com/mizupanel/mizupanel/internal/server/store"
)

//go:embed install-agent.sh
var agentInstallScript []byte

//go:embed install-agent.ps1
var windowsAgentInstallScript []byte

//go:embed uninstall-agent.sh
var agentUninstallScript []byte

//go:embed uninstall-agent.ps1
var windowsAgentUninstallScript []byte

type Dependencies struct {
	Nodes                  *store.NodeStore
	Metrics                *store.MetricStore
	ProcessSnapshots       *store.ProcessSnapshotStore
	DockerSnapshots        *store.DockerSnapshotStore
	AgentTokens            *store.AgentTokenStore
	Settings               *store.SettingsStore
	Alerts                 *store.AlertStore
	AgentToken             string
	PublicURL              string
	Interval               int
	StaticDir              string
	DownloadDir            string
	EnableTerminal         bool
	MetricsRetention       time.Duration
	AlertingEnabled        bool
	AlertCheckInterval     time.Duration
	Debug                  bool
	SSHJobs                *sshops.Manager
	SSHRunner              sshops.Runner
	SSHJobTimeout          time.Duration
	SSHInstallWaitTimeout  time.Duration
	SSHInstallPollInterval time.Duration
	AdminAuth              api.AuthConfig
}

func NewHandler(deps Dependencies) http.Handler {
	mux := http.NewServeMux()
	installAuth := agenthub.NewInstallAuthStore()
	hub := agenthub.NewHandler(deps.Nodes, deps.Metrics, agenthub.Options{
		AgentToken:       deps.AgentToken,
		InstallAuth:      installAuth,
		AgentTokens:      deps.AgentTokens,
		ProcessSnapshots: deps.ProcessSnapshots,
		DockerSnapshots:  deps.DockerSnapshots,
		Interval:         deps.Interval,
		Debug:            deps.Debug,
	})

	// K8s 服务层
	k8sStore := k8s.NewStore(deps.Nodes.DB())
	k8sService := k8s.NewService(k8sStore, hub)
	k8sService.SetDebug(deps.Debug)

	auth := api.NewAuthenticator(deps.AdminAuth)
	apiRouter := api.NewRouter(deps.Nodes, deps.Metrics, deps.ProcessSnapshots, deps.DockerSnapshots, deps.Alerts, hub, k8sService, api.TerminalConfig{Enabled: deps.EnableTerminal}, api.SettingsConfig{Store: deps.Settings, DefaultMetricsRetention: deps.MetricsRetention}, auth)

	// Start alerting engine if enabled
	if deps.AlertingEnabled && deps.Alerts != nil {
		engine := alerting.NewEngine(deps.Alerts, deps.Metrics, deps.Nodes)
		if err := engine.Initialize(context.Background()); err != nil {
			log.Printf("Warning: failed to initialize alerting engine state: %v", err)
		}
		go startAlertingEngine(context.Background(), engine, deps.AlertCheckInterval)
	}

	sshJobs := deps.SSHJobs
	if sshJobs == nil {
		sshJobs = sshops.NewManager()
	}
	sshRunner := deps.SSHRunner
	if sshRunner == nil {
		sshRunner = sshops.NewCommandRunner()
	}
	mux.Handle("/api/auth/", apiRouter)
	mux.Handle("/api/system/", apiRouter)
	mux.Handle("/api/settings", apiRouter)
	mux.Handle("/api/nodes", apiRouter)
	mux.Handle("/api/alerts/", apiRouter)
	mux.Handle("/api/k8s/", apiRouter)
	mux.HandleFunc("/api/nodes/", auth.Require(func(w http.ResponseWriter, r *http.Request) {
		if handleSSHUninstallRoute(w, r, deps.Nodes, hub, sshJobs, sshRunner, deps.PublicURL, deps.SSHJobTimeout) {
			return
		}
		apiRouter.ServeHTTP(w, r)
	}))
	mux.HandleFunc("/api/install/command", auth.Require(func(w http.ResponseWriter, r *http.Request) {
		handleInstallCommand(w, r, deps.PublicURL, installAuth)
	}))
	mux.HandleFunc("/api/install/ssh", auth.Require(func(w http.ResponseWriter, r *http.Request) {
		handleSSHInstall(w, r, deps.Nodes, sshJobs, sshRunner, deps.PublicURL, installAuth, deps.SSHJobTimeout, deps.SSHInstallWaitTimeout, deps.SSHInstallPollInterval)
	}))
	mux.HandleFunc("/api/install/ssh/", auth.Require(func(w http.ResponseWriter, r *http.Request) {
		handleSSHInstallEvents(w, r, sshJobs)
	}))
	mux.Handle("/api/agent/ws", hub)
	mux.HandleFunc("/scripts/install-agent.sh", agentInstallScriptHandler)
	mux.HandleFunc("/scripts/install-agent.ps1", windowsAgentInstallScriptHandler)
	mux.HandleFunc("/scripts/uninstall-agent.sh", agentUninstallScriptHandler)
	mux.HandleFunc("/scripts/uninstall-agent.ps1", windowsAgentUninstallScriptHandler)
	if deps.DownloadDir != "" {
		mux.Handle("/downloads/", downloadHandler(deps.DownloadDir))
	}
	if deps.StaticDir != "" {
		mux.Handle("/", staticHandler(deps.StaticDir))
	}
	return mux
}

func agentInstallScriptHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(agentInstallScript)
}

func windowsAgentInstallScriptHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(windowsAgentInstallScript)
}

func agentUninstallScriptHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(agentUninstallScript)
}

func windowsAgentUninstallScriptHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(windowsAgentUninstallScript)
}

type installPlatform string

type installMode string

const (
	installPlatformLinux   installPlatform = "linux"
	installPlatformWindows installPlatform = "windows"
	installModeNormal      installMode     = "normal"
	installModeOps         installMode     = "ops"
)

func parseInstallPlatform(value string) (installPlatform, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", string(installPlatformLinux):
		return installPlatformLinux, true
	case string(installPlatformWindows):
		return installPlatformWindows, true
	default:
		return "", false
	}
}

func parseInstallMode(value string) (installMode, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", string(installModeNormal):
		return installModeNormal, true
	case string(installModeOps):
		return installModeOps, true
	default:
		return "", false
	}
}

type sshInstallRequest struct {
	sshops.SSHRequest
	NodeID         string `json:"node_id"`
	Name           string `json:"name"`
	EnableTerminal bool   `json:"enable_terminal"`
	EnableDocker   bool   `json:"enable_docker"`
	Mode           string `json:"mode"`
}

func handleSSHInstall(w http.ResponseWriter, r *http.Request, nodes *store.NodeStore, jobs *sshops.Manager, runner sshops.Runner, publicURL string, installAuth *agenthub.InstallAuthStore, jobTimeout, waitTimeout, pollInterval time.Duration) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !sameOrigin(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var request sshInstallRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := sshops.ValidateSSHRequest(&request.SSHRequest); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	installToken, err := randomToken()
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !installAuth.CreateInstallToken(installToken) {
		http.Error(w, "too many install tokens", http.StatusTooManyRequests)
		return
	}
	baseURL, wsURL := installURLs(publicURL, r)
	installRequest := sshops.InstallRequest{
		SSHRequest:     request.SSHRequest,
		BaseURL:        baseURL,
		ServerURL:      wsURL,
		Token:          installToken,
		NodeID:         request.NodeID,
		Name:           request.Name,
		EnableTerminal: true,
		EnableDocker:   true,
		Mode:           string(installModeOps),
	}
	secrets := []string{request.Password, request.PrivateKey, request.Passphrase, installToken}
	if waitTimeout == 0 {
		waitTimeout = 60 * time.Second
	}
	if pollInterval == 0 {
		pollInterval = time.Second
	}
	jobCtx, cancelJob := sshJobContext(r.Context(), jobTimeout)
	jobID := jobs.Start(jobCtx, secrets, func(ctx context.Context, emit sshops.EmitFunc) error {
		defer cancelJob()
		emit(sshops.ProgressEvent{Step: "create_token", Label: "创建 install_token", Status: sshops.ProgressSuccess, Message: "一次性安装 token 已创建"})
		resolvedNodeID, err := runner.Install(ctx, installRequest, emit)
		if err != nil {
			return err
		}
		waitNodeID := installRequest.NodeID
		if strings.TrimSpace(waitNodeID) == "" {
			waitNodeID = resolvedNodeID
		}
		return waitForInstalledNode(ctx, nodes, waitNodeID, waitTimeout, pollInterval, emit)
	})
	writeJSON(w, http.StatusAccepted, map[string]string{"job_id": jobID})
}

func sshJobContext(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout == 0 {
		timeout = 5 * time.Minute
	}
	return context.WithTimeout(context.WithoutCancel(parent), timeout)
}

func waitForInstalledNode(ctx context.Context, nodes *store.NodeStore, nodeID string, timeout, pollInterval time.Duration, emit sshops.EmitFunc) error {
	if nodes == nil || strings.TrimSpace(nodeID) == "" {
		return fmt.Errorf("Agent 安装完成但缺少节点 ID，无法确认是否连回")
	}
	emit(sshops.ProgressEvent{Step: "wait_agent", Label: "等待 Agent 上线", Status: sshops.ProgressRunning, Message: "正在等待 Agent 首次连回 MizuPanel"})
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		node, err := nodes.Get(waitCtx, nodeID)
		if err == nil && node.Status == "online" {
			emit(sshops.ProgressEvent{Step: "wait_agent", Label: "等待 Agent 上线", Status: sshops.ProgressSuccess, Message: "Agent 已连接，安装成功"})
			return nil
		}
		select {
		case <-waitCtx.Done():
			return fmt.Errorf("Agent 安装完成但超时未连回 MizuPanel，请检查 server_url、防火墙或 Agent 日志")
		case <-ticker.C:
		}
	}
}

func handleSSHInstallEvents(w http.ResponseWriter, r *http.Request, jobs *sshops.Manager) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/install/ssh/"), "/"), "/")
	if len(parts) != 2 || parts[1] != "events" {
		http.NotFound(w, r)
		return
	}
	handleSSHJobEvents(w, r, jobs, parts[0])
}

func handleSSHJobEvents(w http.ResponseWriter, r *http.Request, jobs *sshops.Manager, jobID string) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	history, updates, ok := jobs.Subscribe(jobID)
	if !ok {
		http.Error(w, "job not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, _ := w.(http.Flusher)
	writeEvent := func(event sshops.ProgressEvent) {
		data, _ := json.Marshal(event)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
		if flusher != nil {
			flusher.Flush()
		}
	}
	for _, event := range history {
		writeEvent(event)
	}
	for {
		select {
		case event, ok := <-updates:
			if !ok {
				return
			}
			writeEvent(event)
		case <-r.Context().Done():
			return
		}
	}
}

type sshUninstallRequest struct {
	sshops.SSHRequest
	RemoveNodeRecord bool `json:"remove_node_record"`
}

func handleSSHUninstallRoute(w http.ResponseWriter, r *http.Request, nodes *store.NodeStore, disconnecter agenthubDisconnecter, jobs *sshops.Manager, runner sshops.Runner, publicURL string, jobTimeout time.Duration) bool {
	path := strings.TrimPrefix(r.URL.Path, "/api/nodes/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 || parts[1] != "ssh-uninstall" {
		return false
	}
	nodeID := parts[0]
	if len(parts) == 4 && parts[2] != "" && parts[3] == "events" {
		handleSSHJobEvents(w, r, jobs, parts[2])
		return true
	}
	if len(parts) != 2 {
		http.NotFound(w, r)
		return true
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return true
	}
	if !sameOrigin(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return true
	}
	if _, err := nodes.Get(r.Context(), nodeID); err != nil {
		http.Error(w, "node not found", http.StatusNotFound)
		return true
	}
	var request sshUninstallRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return true
	}
	if err := sshops.ValidateSSHRequest(&request.SSHRequest); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return true
	}
	baseURL, _ := installURLs(publicURL, r)
	uninstallRequest := sshops.UninstallRequest{SSHRequest: request.SSHRequest, BaseURL: baseURL, NodeID: nodeID, RemoveNodeRecord: request.RemoveNodeRecord}
	secrets := []string{request.Password, request.PrivateKey, request.Passphrase}
	jobCtx, cancelJob := sshJobContext(r.Context(), jobTimeout)
	jobID := jobs.Start(jobCtx, secrets, func(ctx context.Context, emit sshops.EmitFunc) error {
		defer cancelJob()
		if err := runner.Uninstall(ctx, uninstallRequest, emit); err != nil {
			return err
		}
		if request.RemoveNodeRecord {
			emit(sshops.ProgressEvent{Step: "remove_record", Label: "移除面板记录", Status: sshops.ProgressRunning, Message: "正在移除面板节点记录"})
			if err := nodes.Delete(ctx, nodeID); err != nil {
				return err
			}
			if disconnecter != nil {
				disconnecter.DisconnectNode(nodeID)
			}
			emit(sshops.ProgressEvent{Step: "remove_record", Label: "移除面板记录", Status: sshops.ProgressSuccess, Message: "面板节点记录已移除"})
		}
		return nil
	})
	writeJSON(w, http.StatusAccepted, map[string]string{"job_id": jobID})
	return true
}

type agenthubDisconnecter interface {
	DisconnectNode(nodeID string)
}

func handleInstallCommand(w http.ResponseWriter, r *http.Request, publicURL string, installAuth *agenthub.InstallAuthStore) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	platform, ok := parseInstallPlatform(r.URL.Query().Get("platform"))
	if !ok {
		http.Error(w, "unsupported install platform", http.StatusBadRequest)
		return
	}
	installToken, err := randomToken()
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !installAuth.CreateInstallToken(installToken) {
		http.Error(w, "too many install tokens", http.StatusTooManyRequests)
		return
	}
	baseURL, wsURL := installURLs(publicURL, r)
	enableDocker := platform == installPlatformLinux
	enableTerminal := platform == installPlatformLinux
	command := linuxInstallCommand(baseURL, wsURL, installToken, enableDocker, enableTerminal, installModeOps)
	if platform == installPlatformWindows {
		command = windowsInstallCommand(baseURL, wsURL, installToken)
	}
	writeJSON(w, http.StatusOK, map[string]string{"command": command, "install_token": installToken})
}

func linuxInstallCommand(baseURL, wsURL, installToken string, enableDocker bool, enableTerminal bool, mode installMode) string {
	lines := []string{
		fmt.Sprintf("curl -fsSL %s -o install-agent.sh \\", shellQuote(baseURL+"/scripts/install-agent.sh")),
		"  && chmod +x install-agent.sh \\",
		"  && ./install-agent.sh \\",
		fmt.Sprintf("    --binary-base-url %s \\", shellQuote(baseURL+"/downloads")),
		fmt.Sprintf("    --server-url %s \\", shellQuote(wsURL)),
		fmt.Sprintf("    --token %s \\", shellQuote(installToken)),
		fmt.Sprintf("    --mode %s \\", shellQuote(string(mode))),
		"    --node-id \"$(hostname)\" \\",
		"    --name \"$(hostname)\"",
	}
	for _, option := range []struct {
		enabled bool
		flag    string
	}{
		{enabled: enableDocker, flag: "--enable-docker"},
		{enabled: enableTerminal, flag: "--enable-terminal"},
	} {
		if option.enabled {
			lines[len(lines)-1] += " \\"
			lines = append(lines, "    "+option.flag)
		}
	}
	return strings.Join(lines, "\n")
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func powershellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func windowsInstallCommand(baseURL, wsURL, installToken string) string {
	return strings.Join([]string{
		fmt.Sprintf("powershell -NoProfile -ExecutionPolicy Bypass -Command \"`$ErrorActionPreference='Stop'; `$script = Join-Path `$env:TEMP ('mizupanel-install-' + [guid]::NewGuid().ToString() + '.ps1'); Invoke-WebRequest -Uri %s -UseBasicParsing -OutFile `$script -ErrorAction Stop; & `$script `", powershellQuote(baseURL+"/scripts/install-agent.ps1")),
		fmt.Sprintf("    -BinaryBaseUrl %s `", powershellQuote(baseURL+"/downloads")),
		fmt.Sprintf("    -ServerUrl %s `", powershellQuote(wsURL)),
		fmt.Sprintf("    -Token %s `", powershellQuote(installToken)),
		"    -NodeId `$env:COMPUTERNAME `",
		"    -Name `$env:COMPUTERNAME\"",
	}, "\n")
}

func installURLs(publicURL string, r *http.Request) (string, string) {
	baseURL := strings.TrimRight(publicURL, "/")
	if baseURL == "" {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		baseURL = fmt.Sprintf("%s://%s", scheme, r.Host)
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return baseURL, baseURL + "/api/agent/ws"
	}
	wsScheme := "ws"
	if parsed.Scheme == "https" {
		wsScheme = "wss"
	}
	basePath := strings.TrimRight(parsed.Path, "/")
	parsed.Scheme = wsScheme
	parsed.Path = basePath + "/api/agent/ws"
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return baseURL, parsed.String()
}

func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return false
	}
	expectedScheme := "http"
	if r.TLS != nil {
		expectedScheme = "https"
	}
	return strings.EqualFold(parsed.Scheme, expectedScheme) && strings.EqualFold(parsed.Host, r.Host)
}

func randomToken() (string, error) {
	var bytes [24]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func downloadHandler(dir string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/downloads/")
		if name == "" || name != filepath.Base(name) {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		http.ServeFile(w, r, filepath.Join(dir, name))
	})
}

func staticHandler(dir string) http.Handler {
	files := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := filepath.Join(dir, filepath.Clean(r.URL.Path))
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			files.ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(dir, "index.html"))
	})
}

func startAlertingEngine(ctx context.Context, engine *alerting.Engine, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := engine.CheckRules(ctx); err != nil {
				// Log error but continue
				fmt.Fprintf(os.Stderr, "alerting engine error: %v\n", err)
			}
		}
	}
}
