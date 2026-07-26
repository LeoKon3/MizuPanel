package uptime

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/mizupanel/mizupanel/internal/server/store"
)

const (
	MonitorTypeHTTP = "http"
	MonitorTypeTCP  = "tcp"

	DefaultIntervalSeconds        = 60
	DefaultTimeoutSeconds         = 5
	DefaultFailureThreshold       = 3
	DefaultExpectedStatusMin      = 200
	DefaultExpectedStatusMax      = 399
	DefaultTLSExpiryThresholdDays = 30

	MaxMonitorNameRunes       = 120
	MaxMonitorTargetBytes     = 2048
	MinIntervalSeconds        = 30
	MaxIntervalSeconds        = 86400
	MinTimeoutSeconds         = 1
	MaxTimeoutSeconds         = 30
	MinFailureThreshold       = 1
	MaxFailureThreshold       = 10
	MinTLSExpiryThresholdDays = 1
	MaxTLSExpiryThresholdDays = 365
	MaxNotificationChannels   = 10
	MaxNotificationValueBytes = 4096
)

var ErrInvalidMonitor = errors.New("invalid uptime monitor")

func ApplyDefaults(monitor *store.UptimeMonitor) {
	monitor.Name = strings.TrimSpace(monitor.Name)
	monitor.Type = strings.ToLower(strings.TrimSpace(monitor.Type))
	monitor.Target = strings.TrimSpace(monitor.Target)
	if monitor.Type == "" {
		monitor.Type = MonitorTypeHTTP
	}
	if monitor.IntervalSeconds == 0 {
		monitor.IntervalSeconds = DefaultIntervalSeconds
	}
	if monitor.TimeoutSeconds == 0 {
		monitor.TimeoutSeconds = DefaultTimeoutSeconds
	}
	if monitor.FailureThreshold == 0 {
		monitor.FailureThreshold = DefaultFailureThreshold
	}
	if monitor.ExpectedStatusMin == 0 {
		monitor.ExpectedStatusMin = DefaultExpectedStatusMin
	}
	if monitor.ExpectedStatusMax == 0 {
		monitor.ExpectedStatusMax = DefaultExpectedStatusMax
	}
	if monitor.TLSExpiryThresholdDays == 0 {
		monitor.TLSExpiryThresholdDays = DefaultTLSExpiryThresholdDays
	}
	if monitor.NotificationChannels == nil {
		monitor.NotificationChannels = make([]store.NotificationChannel, 0)
	}
}

func ValidateMonitor(monitor *store.UptimeMonitor) error {
	ApplyDefaults(monitor)
	if monitor.Name == "" || !utf8.ValidString(monitor.Name) || utf8.RuneCountInString(monitor.Name) > MaxMonitorNameRunes {
		return invalidMonitor("拨测名称不能为空且最多 %d 个字符", MaxMonitorNameRunes)
	}
	if monitor.Target == "" || len(monitor.Target) > MaxMonitorTargetBytes {
		return invalidMonitor("拨测目标不能为空且最多 %d 字节", MaxMonitorTargetBytes)
	}
	if monitor.IntervalSeconds < MinIntervalSeconds || monitor.IntervalSeconds > MaxIntervalSeconds {
		return invalidMonitor("检测间隔必须在 %d-%d 秒之间", MinIntervalSeconds, MaxIntervalSeconds)
	}
	if monitor.TimeoutSeconds < MinTimeoutSeconds || monitor.TimeoutSeconds > MaxTimeoutSeconds || monitor.TimeoutSeconds >= monitor.IntervalSeconds {
		return invalidMonitor("超时时间必须在 %d-%d 秒之间且小于检测间隔", MinTimeoutSeconds, MaxTimeoutSeconds)
	}
	if monitor.FailureThreshold < MinFailureThreshold || monitor.FailureThreshold > MaxFailureThreshold {
		return invalidMonitor("连续失败阈值必须在 %d-%d 次之间", MinFailureThreshold, MaxFailureThreshold)
	}
	if monitor.ExpectedStatusMin < 100 || monitor.ExpectedStatusMin > 599 || monitor.ExpectedStatusMax < 100 || monitor.ExpectedStatusMax > 599 || monitor.ExpectedStatusMin > monitor.ExpectedStatusMax {
		return invalidMonitor("HTTP 状态码范围必须在 100-599 之间且起始值不能大于结束值")
	}
	if monitor.TLSExpiryThresholdDays < MinTLSExpiryThresholdDays || monitor.TLSExpiryThresholdDays > MaxTLSExpiryThresholdDays {
		return invalidMonitor("证书预警天数必须在 %d-%d 天之间", MinTLSExpiryThresholdDays, MaxTLSExpiryThresholdDays)
	}
	switch monitor.Type {
	case MonitorTypeHTTP:
		if err := validateHTTPTarget(monitor.Target); err != nil {
			return err
		}
	case MonitorTypeTCP:
		if err := validateTCPTarget(monitor.Target); err != nil {
			return err
		}
	default:
		return invalidMonitor("拨测类型仅支持 http 或 tcp")
	}
	if err := validateNotificationChannels(monitor.NotificationChannels); err != nil {
		return err
	}
	return nil
}

func validateHTTPTarget(target string) error {
	parsed, err := url.Parse(target)
	if err != nil || !parsed.IsAbs() || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return invalidMonitor("HTTP 目标必须是完整的 http:// 或 https:// 地址")
	}
	if parsed.User != nil {
		return invalidMonitor("HTTP 目标不能包含用户名或密码")
	}
	if parsed.Fragment != "" {
		return invalidMonitor("HTTP 目标不能包含 URL 片段")
	}
	return nil
}

func validateTCPTarget(target string) error {
	host, portValue, err := net.SplitHostPort(target)
	if err != nil || strings.TrimSpace(host) == "" {
		return invalidMonitor("TCP 目标必须使用 host:port 格式，IPv6 地址需要使用方括号")
	}
	port, err := strconv.Atoi(portValue)
	if err != nil || port < 1 || port > 65535 {
		return invalidMonitor("TCP 端口必须在 1-65535 之间")
	}
	return nil
}

func validateNotificationChannels(channels []store.NotificationChannel) error {
	if len(channels) > MaxNotificationChannels {
		return invalidMonitor("通知渠道最多 %d 个", MaxNotificationChannels)
	}
	for _, channel := range channels {
		if channel.Type != "webhook" && channel.Type != "dingtalk" && channel.Type != "feishu" {
			return invalidMonitor("通知渠道类型仅支持 webhook、dingtalk 或 feishu")
		}
		if len(channel.Headers) > 0 {
			return invalidMonitor("服务拨测不支持自定义通知请求头")
		}
		if len(channel.WebhookURL) > MaxNotificationValueBytes || len(channel.Secret) > MaxNotificationValueBytes {
			return invalidMonitor("通知渠道配置过长")
		}
		parsed, err := url.Parse(strings.TrimSpace(channel.WebhookURL))
		if err != nil || !parsed.IsAbs() || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil {
			return invalidMonitor("通知渠道必须使用完整的 http:// 或 https:// Webhook 地址")
		}
	}
	return nil
}

func invalidMonitor(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidMonitor, fmt.Sprintf(format, args...))
}
