package upgrade

import (
	"crypto/sha256"
	"debug/elf"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/mizupanel/mizupanel/internal/protocol"
)

type Upgrader struct {
	serverURL, currentVersion, mode, executable string
	client                                      *http.Client
	restart                                     func() error
	sleep                                       func(time.Duration)
	scheduleRecovery                            func(string, string, string, string) error
	cancelRecovery                              func(string)
	mu                                          sync.Mutex
	running                                     bool
}

type Reporter = func(protocol.AgentUpgradeResponse)

func New(serverURL, currentVersion, mode string) *Upgrader {
	executable, _ := os.Executable()
	return &Upgrader{serverURL: serverURL, currentVersion: currentVersion, mode: mode, executable: executable, client: &http.Client{Timeout: 2 * time.Minute}, restart: restartAgent, sleep: time.Sleep, scheduleRecovery: scheduleRecovery, cancelRecovery: cancelRecovery}
}

func (u *Upgrader) ConfirmSuccessfulStart() {
	_, marker, _, _ := u.recoveryPaths()
	cancel := u.cancelRecovery
	if cancel == nil {
		cancel = cancelRecovery
	}
	cancel(marker)
}

func (u *Upgrader) Start(request protocol.AgentUpgradeRequest, report Reporter) protocol.AgentUpgradeResponse {
	if err := u.validate(request); err != nil {
		return protocol.AgentUpgradeResponse{Code: "invalid_upgrade", Error: err.Error()}
	}
	u.mu.Lock()
	if u.running {
		u.mu.Unlock()
		return protocol.AgentUpgradeResponse{Code: "upgrade_in_progress", Error: "Agent 升级正在进行中。"}
	}
	u.running = true
	u.mu.Unlock()
	go u.run(request, report)
	return protocol.AgentUpgradeResponse{Accepted: true, Stage: "preparing", Message: "Agent 升级已开始。"}
}

func (u *Upgrader) validate(request protocol.AgentUpgradeRequest) error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("当前系统暂不支持一键升级")
	}
	if u.mode != "ops" {
		return fmt.Errorf("普通模式 Agent 请使用安装命令升级")
	}
	if request.TargetVersion == "" || request.TargetVersion == u.currentVersion {
		return fmt.Errorf("目标版本无效")
	}
	if len(request.SHA256) != 64 {
		return fmt.Errorf("升级包校验值无效")
	}
	if _, err := hex.DecodeString(request.SHA256); err != nil {
		return fmt.Errorf("升级包校验值无效")
	}
	server, err := url.Parse(u.serverURL)
	if err != nil {
		return fmt.Errorf("Server 地址无效")
	}
	download, err := url.Parse(request.DownloadURL)
	if err != nil {
		return fmt.Errorf("下载地址无效")
	}
	if server.Scheme == "ws" {
		server.Scheme = "http"
	}
	if server.Scheme == "wss" {
		server.Scheme = "https"
	}
	if download.Scheme != server.Scheme || download.Host != server.Host {
		return fmt.Errorf("升级包必须来自当前 Server")
	}
	if download.Path != "/downloads/mizupanel-agent-linux-"+runtime.GOARCH || download.RawQuery != "" || download.Fragment != "" {
		return fmt.Errorf("升级包路径无效")
	}
	return nil
}

func (u *Upgrader) run(request protocol.AgentUpgradeRequest, report Reporter) {
	defer func() { u.mu.Lock(); u.running = false; u.mu.Unlock() }()
	fail := func(code string, err error) {
		if report != nil {
			report(protocol.AgentUpgradeResponse{Stage: "failed", Code: code, Error: err.Error()})
		}
	}
	root := filepath.Dir(filepath.Dir(u.executable))
	tempDir, backupDir := filepath.Join(root, "var", "upgrade"), filepath.Join(root, "var", "backups")
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		fail("prepare_failed", fmt.Errorf("创建升级临时目录失败: %w", err))
		return
	}
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		fail("prepare_failed", fmt.Errorf("创建升级备份目录失败: %w", err))
		return
	}
	temp, err := os.CreateTemp(tempDir, "mizupanel-agent-*")
	if err != nil {
		fail("prepare_failed", fmt.Errorf("创建升级临时文件失败: %w", err))
		return
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	response, err := u.client.Get(request.DownloadURL)
	if err != nil {
		temp.Close()
		fail("download_failed", fmt.Errorf("下载升级包失败: %w", err))
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		temp.Close()
		fail("download_failed", fmt.Errorf("下载升级包失败: HTTP %d", response.StatusCode))
		return
	}
	hash := sha256.New()
	const maxDownloadBytes = int64(256 * 1024 * 1024)
	written, err := io.Copy(io.MultiWriter(temp, hash), io.LimitReader(response.Body, maxDownloadBytes+1))
	if err != nil {
		temp.Close()
		fail("download_failed", fmt.Errorf("保存升级包失败: %w", err))
		return
	}
	if written > maxDownloadBytes {
		temp.Close()
		fail("package_too_large", fmt.Errorf("升级包超过 256 MiB 限制"))
		return
	}
	if err := temp.Close(); err != nil {
		fail("prepare_failed", fmt.Errorf("写入升级包失败: %w", err))
		return
	}
	if !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), request.SHA256) {
		fail("checksum_mismatch", fmt.Errorf("升级包 SHA-256 校验失败"))
		return
	}
	if err := os.Chmod(tempPath, 0755); err != nil {
		fail("prepare_failed", fmt.Errorf("设置升级包权限失败: %w", err))
		return
	}
	if err := validateLinuxExecutable(tempPath); err != nil {
		fail("invalid_package", err)
		return
	}
	backup := filepath.Join(backupDir, "mizupanel-agent.previous")
	if err := copyFile(u.executable, backup); err != nil {
		fail("backup_failed", fmt.Errorf("备份当前 Agent 失败: %w", err))
		return
	}
	script, marker, _, _ := u.recoveryPaths()
	if err := writeRollbackScript(script); err != nil {
		fail("recovery_setup_failed", fmt.Errorf("创建升级恢复脚本失败: %w", err))
		return
	}
	if err := os.WriteFile(marker, []byte(request.TargetVersion+"\n"), 0600); err != nil {
		fail("recovery_setup_failed", fmt.Errorf("创建升级恢复标记失败: %w", err))
		return
	}
	schedule := u.scheduleRecovery
	if schedule == nil {
		schedule = scheduleRecovery
	}
	if err := schedule(script, marker, backup, u.executable); err != nil {
		_ = os.Remove(marker)
		fail("recovery_setup_failed", fmt.Errorf("注册升级恢复任务失败: %w", err))
		return
	}
	if err := os.Rename(tempPath, u.executable); err != nil {
		u.cancelArmedRecovery(marker)
		fail("replace_failed", fmt.Errorf("替换 Agent 文件失败: %w", err))
		return
	}
	if u.sleep != nil {
		u.sleep(750 * time.Millisecond)
	}
	restart := u.restart
	if restart == nil {
		restart = restartAgent
	}
	if err := restart(); err != nil {
		if restoreErr := copyFile(backup, u.executable); restoreErr != nil {
			fail("restart_failed", fmt.Errorf("重启 Agent 失败且恢复旧版本失败: %v; %w", err, restoreErr))
			return
		}
		u.cancelArmedRecovery(marker)
		_ = restart()
		fail("restart_failed", fmt.Errorf("重启新版本 Agent 失败，已恢复旧版本: %w", err))
	}
}

func (u *Upgrader) recoveryPaths() (script string, marker string, backup string, root string) {
	root = filepath.Dir(filepath.Dir(u.executable))
	return filepath.Join(root, "var", "upgrade", "rollback.sh"), filepath.Join(root, "var", "upgrade", "pending"), filepath.Join(root, "var", "backups", "mizupanel-agent.previous"), root
}

func (u *Upgrader) cancelArmedRecovery(marker string) {
	cancel := u.cancelRecovery
	if cancel == nil {
		cancel = cancelRecovery
	}
	cancel(marker)
}

func validateLinuxExecutable(path string) error {
	info, err := os.Stat(path)
	if err != nil || info.Size() == 0 {
		return errors.New("升级包不是有效的可执行文件")
	}
	file, err := elf.Open(path)
	if err != nil {
		return errors.New("升级包不是有效的 Linux ELF 文件")
	}
	defer file.Close()
	expected := elf.EM_NONE
	switch runtime.GOARCH {
	case "amd64":
		expected = elf.EM_X86_64
	case "arm64":
		expected = elf.EM_AARCH64
	}
	if expected == elf.EM_NONE || file.Machine != expected {
		return errors.New("升级包架构与当前 Agent 不匹配")
	}
	if file.Type != elf.ET_EXEC && file.Type != elf.ET_DYN {
		return errors.New("升级包不是可执行的 Linux ELF 文件")
	}
	return nil
}

const rollbackScript = `#!/bin/sh
set -eu
marker=$1
backup=$2
executable=$3
[ -f "$marker" ] || exit 0
install -m 0755 "$backup" "$executable.rollback"
mv -f "$executable.rollback" "$executable"
rm -f "$marker"
systemctl restart mizupanel-agent
`

func writeRollbackScript(path string) error {
	return os.WriteFile(path, []byte(rollbackScript), 0700)
}

func scheduleRecovery(script string, marker string, backup string, executable string) error {
	_ = exec.Command("systemctl", "stop", "mizupanel-agent-upgrade-rollback.timer").Run()
	_ = exec.Command("systemctl", "stop", "mizupanel-agent-upgrade-rollback.service").Run()
	_ = exec.Command("systemctl", "reset-failed", "mizupanel-agent-upgrade-rollback.timer", "mizupanel-agent-upgrade-rollback.service").Run()
	return exec.Command("systemd-run", "--unit=mizupanel-agent-upgrade-rollback", "--on-active=2min", "/bin/sh", script, marker, backup, executable).Run()
}

func cancelRecovery(marker string) {
	_ = os.Remove(marker)
	_ = exec.Command("systemctl", "stop", "mizupanel-agent-upgrade-rollback.timer").Run()
	_ = exec.Command("systemctl", "reset-failed", "mizupanel-agent-upgrade-rollback.timer", "mizupanel-agent-upgrade-rollback.service").Run()
}

func restartAgent() error {
	return exec.Command("systemctl", "restart", "mizupanel-agent").Run()
}

func copyFile(source, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	temp, err := os.CreateTemp(filepath.Dir(target), ".upgrade-copy-*")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer os.Remove(name)
	if _, err := io.Copy(temp, in); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Chmod(0755); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(name, target)
}
