package deploy

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallAgentScriptGeneratesConfigAndInstallsService(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	script := filepath.Join(repoRoot, "scripts", "install-agent.sh")
	binary := filepath.Join(t.TempDir(), "mizupanel-agent")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatalf("write binary: %v", err)
	}
	dest := t.TempDir()
	fakeBin := t.TempDir()
	if err := os.WriteFile(filepath.Join(fakeBin, "systemctl"), []byte("#!/bin/sh\necho systemctl should not run in dest-root mode >&2\nexit 1\n"), 0755); err != nil {
		t.Fatalf("write fake systemctl: %v", err)
	}

	output, err := runCommand(t, script, map[string]string{"PATH": fakeBin + string(os.PathListSeparator) + os.Getenv("PATH")},
		"--dest-root", dest,
		"--binary", binary,
		"--server-url", "ws://panel.example.com:8080/api/agent/ws",
		"--token", "secret-token",
		"--node-id", "oracle-sg-01",
		"--name", "Oracle SG",
		"--interval", "5s",
	)
	if err != nil {
		t.Fatalf("install script failed: %v\n%s", err, output)
	}

	config, err := os.ReadFile(filepath.Join(dest, "usr", "local", "mizupanel", "etc", "agent.yaml"))
	if err != nil {
		t.Fatalf("read generated config: %v", err)
	}
	wantConfig := []string{
		`server:`,
		`  url: "ws://panel.example.com:8080/api/agent/ws"`,
		`  token: "secret-token"`,
		`node:`,
		`  name: "Oracle SG"`,
		`runtime:`,
		`  interval: "5s"`,
		`  mode: "normal"`,
		`features:`,
		`  docker: false`,
		`  terminal: false`,
	}
	for _, want := range wantConfig {
		if !strings.Contains(string(config), want) {
			t.Fatalf("generated config missing %q:\n%s", want, config)
		}
	}
	info, err := os.Stat(filepath.Join(dest, "usr", "local", "mizupanel", "etc", "agent.yaml"))
	if err != nil {
		t.Fatalf("stat generated config: %v", err)
	}
	if info.Mode().Perm()&0007 != 0 {
		t.Fatalf("agent config is readable by other users: %s", info.Mode().Perm())
	}
	installDir, err := os.Stat(filepath.Join(dest, "usr", "local", "mizupanel"))
	if err != nil {
		t.Fatalf("stat install dir: %v", err)
	}
	if installDir.Mode().Perm()&0022 != 0 {
		t.Fatalf("install dir is writable by group or other users: %s", installDir.Mode().Perm())
	}
	if _, err := os.Stat(filepath.Join(dest, "usr", "local", "mizupanel", "bin", "mizupanel-agent")); err != nil {
		t.Fatalf("agent binary was not installed: %v", err)
	}
	service, err := os.ReadFile(filepath.Join(dest, "etc", "systemd", "system", "mizupanel-agent.service"))
	if err != nil {
		t.Fatalf("read installed service: %v", err)
	}
	template, err := os.ReadFile(filepath.Join(repoRoot, "systemd", "mizupanel-agent.service"))
	if err != nil {
		t.Fatalf("read service template: %v", err)
	}
	_ = template
	if strings.Contains(string(service), "User=root") {
		t.Fatalf("service must not run as root:\n%s", service)
	}
	if !strings.Contains(string(service), "User=mizupanel-agent") {
		t.Fatalf("service must run as mizupanel-agent:\n%s", service)
	}
	if !strings.Contains(string(service), "ExecStart=/usr/local/mizupanel/bin/mizupanel-agent -config /usr/local/mizupanel/etc/agent.yaml") {
		t.Fatalf("service has unexpected ExecStart:\n%s", service)
	}
}

func TestInstallAgentScriptWritesOpsModeServiceAndWarning(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	script := filepath.Join(repoRoot, "scripts", "install-agent.sh")
	binary := filepath.Join(t.TempDir(), "mizupanel-agent")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatalf("write binary: %v", err)
	}
	dest := t.TempDir()

	output, err := runCommand(t, script, nil,
		"--dest-root", dest,
		"--binary", binary,
		"--server-url", "ws://panel.example.com:8080/api/agent/ws",
		"--token", "secret-token",
		"--node-id", "oracle-sg-01",
		"--name", "Oracle SG",
		"--mode", "ops",
	)
	if err != nil {
		t.Fatalf("install script failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "运维模式会以 root 用户运行 Agent") {
		t.Fatalf("ops mode warning missing from output:\n%s", output)
	}
	config, err := os.ReadFile(filepath.Join(dest, "usr", "local", "mizupanel", "etc", "agent.yaml"))
	if err != nil {
		t.Fatalf("read generated config: %v", err)
	}
	if !strings.Contains(string(config), `  mode: "ops"`) {
		t.Fatalf("generated config missing ops mode:\n%s", config)
	}
	service, err := os.ReadFile(filepath.Join(dest, "etc", "systemd", "system", "mizupanel-agent.service"))
	if err != nil {
		t.Fatalf("read installed service: %v", err)
	}
	if !strings.Contains(string(service), "User=root") || !strings.Contains(string(service), "Group=root") {
		t.Fatalf("ops service must run as root:\n%s", service)
	}
}

func TestInstallAgentScriptWritesDockerOptInConfigInDestRootMode(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	script := filepath.Join(repoRoot, "scripts", "install-agent.sh")
	binary := filepath.Join(t.TempDir(), "mizupanel-agent")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatalf("write binary: %v", err)
	}
	dest := t.TempDir()
	fakeBin := t.TempDir()
	if err := os.WriteFile(filepath.Join(fakeBin, "usermod"), []byte("#!/bin/sh\necho usermod should not run in dest-root mode >&2\nexit 1\n"), 0755); err != nil {
		t.Fatalf("write fake usermod: %v", err)
	}

	output, err := runCommand(t, script, map[string]string{"PATH": fakeBin + string(os.PathListSeparator) + os.Getenv("PATH")},
		"--dest-root", dest,
		"--binary", binary,
		"--server-url", "ws://panel.example.com:8080/api/agent/ws",
		"--token", "secret-token",
		"--node-id", "oracle-sg-01",
		"--name", "Oracle SG",
		"--enable-docker",
	)
	if err != nil {
		t.Fatalf("install script failed: %v\n%s", err, output)
	}
	config, err := os.ReadFile(filepath.Join(dest, "usr", "local", "mizupanel", "etc", "agent.yaml"))
	if err != nil {
		t.Fatalf("read generated config: %v", err)
	}
	if !strings.Contains(string(config), `  docker: true`) {
		t.Fatalf("generated config missing Docker opt-in:\n%s", config)
	}
}

func TestInstallAgentScriptWritesTerminalOptInConfigInDestRootMode(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	script := filepath.Join(repoRoot, "scripts", "install-agent.sh")
	binary := filepath.Join(t.TempDir(), "mizupanel-agent")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatalf("write binary: %v", err)
	}
	dest := t.TempDir()

	output, err := runCommand(t, script, nil,
		"--dest-root", dest,
		"--binary", binary,
		"--server-url", "ws://panel.example.com:8080/api/agent/ws",
		"--token", "secret-token",
		"--node-id", "oracle-sg-01",
		"--name", "Oracle SG",
		"--enable-terminal",
	)
	if err != nil {
		t.Fatalf("install script failed: %v\n%s", err, output)
	}
	config, err := os.ReadFile(filepath.Join(dest, "usr", "local", "mizupanel", "etc", "agent.yaml"))
	if err != nil {
		t.Fatalf("read generated config: %v", err)
	}
	if !strings.Contains(string(config), `  terminal: true`) {
		t.Fatalf("generated config missing terminal opt-in:\n%s", config)
	}
}

func TestInstallAgentScriptDockerOptInUsesGroupMembershipWithoutWeakeningSocket(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	content, err := os.ReadFile(filepath.Join(repoRoot, "scripts", "install-agent.sh"))
	if err != nil {
		t.Fatalf("read install script: %v", err)
	}
	script := string(content)
	for _, want := range []string{"--enable-docker", "docker_group()", "safe_docker_group()", "stat -c '%G' /var/run/docker.sock", "usermod -aG \"$DOCKER_GROUP\" mizupanel-agent", "root|wheel|sudo|adm"} {
		if !strings.Contains(script, want) {
			t.Fatalf("install script missing Docker opt-in operation %q", want)
		}
	}
	for _, unsafe := range []string{"chmod 666 /var/run/docker.sock", "chmod a+rw /var/run/docker.sock", "chown mizupanel-agent /var/run/docker.sock"} {
		if strings.Contains(script, unsafe) {
			t.Fatalf("install script weakens Docker socket permissions with %q", unsafe)
		}
	}
}

func TestInstallAgentScriptHardensInstallDirBeforeWritingFiles(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	content, err := os.ReadFile(filepath.Join(repoRoot, "scripts", "install-agent.sh"))
	if err != nil {
		t.Fatalf("read install script: %v", err)
	}
	script := string(content)
	chownIndex := strings.Index(script, "chown root:root \"$INSTALL_DIR\"")
	chmodIndex := strings.Index(script, "chmod 0755 \"$INSTALL_DIR\"")
	binaryTempIndex := strings.Index(script, "mktemp \"$BIN_DIR/mizupanel-agent")
	configTempIndex := strings.Index(script, "mktemp \"$ETC_DIR/agent.yaml")
	if chownIndex == -1 || chmodIndex == -1 || binaryTempIndex == -1 || configTempIndex == -1 {
		t.Fatalf("install script missing expected hardening or temp file operations")
	}
	if chownIndex > binaryTempIndex || chownIndex > configTempIndex || chmodIndex > binaryTempIndex || chmodIndex > configTempIndex {
		t.Fatalf("install dir is hardened after root writes into install dir")
	}
}

func TestInstallAgentScriptRestartsServiceAfterInstall(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	content, err := os.ReadFile(filepath.Join(repoRoot, "scripts", "install-agent.sh"))
	if err != nil {
		t.Fatalf("read install script: %v", err)
	}
	script := string(content)
	if !strings.Contains(script, "systemctl enable mizupanel-agent") || !strings.Contains(script, "systemctl restart mizupanel-agent") || !strings.Contains(script, "systemctl is-active --quiet mizupanel-agent") || !strings.Contains(script, "mizupanel-agent.previous") {
		t.Fatalf("install script does not restart service after enabling it")
	}
}

func TestUninstallAgentScriptPreservesIdentityInDestRootMode(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	script := filepath.Join(repoRoot, "scripts", "uninstall-agent.sh")
	dest := t.TempDir()
	installDir := filepath.Join(dest, "usr", "local", "mizupanel")
	serviceDir := filepath.Join(dest, "etc", "systemd", "system")
	if err := os.MkdirAll(installDir, 0755); err != nil {
		t.Fatalf("mkdir install dir: %v", err)
	}
	if err := os.MkdirAll(serviceDir, 0755); err != nil {
		t.Fatalf("mkdir service dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(installDir, "etc"), 0755); err != nil {
		t.Fatalf("mkdir etc: %v", err)
	}
	if err := os.WriteFile(filepath.Join(installDir, "etc", "agent.yaml"), []byte("token"), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(installDir, "etc", "agent-id"), []byte("7bb62f38-85fd-4b89-9c6d-e2ca51a4c241\n"), 0600); err != nil {
		t.Fatalf("write identity: %v", err)
	}
	if err := os.WriteFile(filepath.Join(serviceDir, "mizupanel-agent.service"), []byte("service"), 0644); err != nil {
		t.Fatalf("write service: %v", err)
	}

	output, err := runCommand(t, script, nil, "--dest-root", dest)
	if err != nil {
		t.Fatalf("uninstall script failed: %v\n%s", err, output)
	}
	if _, err := os.Stat(filepath.Join(installDir, "etc", "agent-id")); err != nil {
		t.Fatalf("identity was not preserved: %v", err)
	}
	if _, err := os.Stat(filepath.Join(serviceDir, "mizupanel-agent.service")); !os.IsNotExist(err) {
		t.Fatalf("service file still exists or stat failed: %v", err)
	}
	if !strings.Contains(output, "MizuPanel agent uninstalled") {
		t.Fatalf("uninstall output missing success message: %s", output)
	}
}

func TestInstallAgentScriptReusesPersistentIdentity(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	script := filepath.Join(repoRoot, "scripts", "install-agent.sh")
	binary := filepath.Join(t.TempDir(), "mizupanel-agent")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()
	args := []string{"--dest-root", dest, "--binary", binary, "--server-url", "ws://panel/api/agent/ws", "--token", "token", "--node-id", "ignored", "--name", "node"}
	if output, err := runCommand(t, script, nil, args...); err != nil {
		t.Fatalf("first install: %v\n%s", err, output)
	}
	identityPath := filepath.Join(dest, "usr", "local", "mizupanel", "etc", "agent-id")
	first, err := os.ReadFile(identityPath)
	if err != nil {
		t.Fatal(err)
	}
	if output, err := runCommand(t, script, nil, args...); err != nil {
		t.Fatalf("second install: %v\n%s", err, output)
	}
	second, err := os.ReadFile(identityPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("identity changed: %q -> %q", first, second)
	}
}

func TestUninstallAgentScriptDoesNotIgnoreRunningServiceStopFailure(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	content, err := os.ReadFile(filepath.Join(repoRoot, "scripts", "uninstall-agent.sh"))
	if err != nil {
		t.Fatalf("read uninstall script: %v", err)
	}
	script := string(content)
	for _, want := range []string{"systemctl is-active --quiet mizupanel-agent", "systemctl stop mizupanel-agent", "systemctl is-enabled --quiet mizupanel-agent", "systemctl disable mizupanel-agent"} {
		if !strings.Contains(script, want) {
			t.Fatalf("uninstall script missing %q", want)
		}
	}
	for _, unsafe := range []string{"systemctl stop mizupanel-agent >/dev/null 2>&1 || true", "systemctl disable mizupanel-agent >/dev/null 2>&1 || true"} {
		if strings.Contains(script, unsafe) {
			t.Fatalf("uninstall script ignores systemctl failure with %q", unsafe)
		}
	}
}

func TestWindowsUninstallAgentScriptRemovesServiceAndInstallDir(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	content, err := os.ReadFile(filepath.Join(repoRoot, "scripts", "uninstall-agent.ps1"))
	if err != nil {
		t.Fatalf("read windows uninstall script: %v", err)
	}
	script := string(content)
	for _, want := range []string{
		"Administrator privileges are required",
		"ServiceName = \"mizupanel-agent\"",
		"Get-Service -Name $ServiceName",
		"Stop-Service -Name $ServiceName -Force",
		"Invoke-Native sc.exe delete",
		"Remove-Item -LiteralPath $InstallDir -Recurse -Force",
		"MizuPanel agent uninstalled",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("windows uninstall script missing %q", want)
		}
	}
}

func TestWindowsInstallAgentScriptInstallsService(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	content, err := os.ReadFile(filepath.Join(repoRoot, "scripts", "install-agent.ps1"))
	if err != nil {
		t.Fatalf("read windows install script: %v", err)
	}
	script := string(content)
	for _, want := range []string{
		"mizupanel-agent-windows-amd64.exe",
		"agent.yaml",
		"ServiceName = \"mizupanel-agent\"",
		"Invoke-Native sc.exe create",
		"function Invoke-Native",
		"$LASTEXITCODE",
		"Invoke-WebRequest -Uri $BinarySource -UseBasicParsing -OutFile $TempBinary",
		"Invoke-Native icacls",
		"Invoke-Native sc.exe config",
		"*S-1-5-32-544",
		"*S-1-5-19",
		"obj= \"NT AUTHORITY\\LocalService\"",
		"-config",
		"Administrator privileges are required",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("windows install script missing %q", want)
		}
	}
	if strings.Contains(script, "Write-Output \"Token") || strings.Contains(script, "Write-Host \"Token") {
		t.Fatalf("windows install script prints token")
	}
}

func TestWindowsInstallAgentScriptStopsExistingServiceBeforeReplacingBinary(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	content, err := os.ReadFile(filepath.Join(repoRoot, "scripts", "install-agent.ps1"))
	if err != nil {
		t.Fatalf("read windows install script: %v", err)
	}
	script := string(content)
	serviceIndex := strings.Index(script, "$ExistingService = Get-Service")
	stopIndex := strings.Index(script, "Stop-Service -Name $ServiceName -Force")
	moveIndex := strings.Index(script, "Move-Item -Force $TempBinary $AgentPath")
	copyIndex := strings.Index(script, "Copy-Item -Force $BinarySource $AgentPath")
	if serviceIndex == -1 || stopIndex == -1 || moveIndex == -1 || copyIndex == -1 {
		t.Fatalf("windows install script missing service stop or binary replacement operations")
	}
	if serviceIndex > moveIndex || serviceIndex > copyIndex || stopIndex > moveIndex || stopIndex > copyIndex {
		t.Fatalf("windows install script replaces the agent binary before stopping the existing service")
	}
}

func runCommand(t *testing.T, path string, env map[string]string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(path, args...)
	cmd.Env = os.Environ()
	for key, value := range env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	output, err := cmd.CombinedOutput()
	return string(output), err
}
