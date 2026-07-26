package uptime

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mizupanel/mizupanel/internal/server/store"
)

const maxHTTPRedirects = 5

var (
	errTooManyRedirects    = errors.New("too many redirects")
	errUnsupportedRedirect = errors.New("unsupported redirect target")
)

type Prober interface {
	Probe(ctx context.Context, monitor store.UptimeMonitor) store.UptimeProbeResult
}

type DialContextFunc func(ctx context.Context, network string, address string) (net.Conn, error)

type NetworkProber struct {
	Transport   http.RoundTripper
	DialContext DialContextFunc
	Now         func() time.Time
}

func NewNetworkProber() *NetworkProber {
	return &NetworkProber{}
}

func (p *NetworkProber) Probe(ctx context.Context, monitor store.UptimeMonitor) store.UptimeProbeResult {
	if err := ValidateMonitor(&monitor); err != nil {
		return store.UptimeProbeResult{Error: "拨测配置无效", CheckedAt: p.now().UTC()}
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(monitor.TimeoutSeconds)*time.Second)
	defer cancel()
	switch monitor.Type {
	case MonitorTypeHTTP:
		return p.probeHTTP(ctx, monitor)
	case MonitorTypeTCP:
		return p.probeTCP(ctx, monitor)
	default:
		return store.UptimeProbeResult{Error: "拨测类型不受支持", CheckedAt: p.now().UTC()}
	}
}

func (p *NetworkProber) probeHTTP(ctx context.Context, monitor store.UptimeMonitor) store.UptimeProbeResult {
	startedAt := p.now()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, monitor.Target, nil)
	if err != nil {
		return store.UptimeProbeResult{Error: "HTTP 请求配置无效", CheckedAt: p.now().UTC()}
	}
	client := &http.Client{
		Transport: p.httpTransport(time.Duration(monitor.TimeoutSeconds) * time.Second),
		Timeout:   time.Duration(monitor.TimeoutSeconds) * time.Second,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			// via contains the initial request plus every redirect already
			// followed. Allow five redirects and reject the sixth.
			if len(via) > maxHTTPRedirects {
				return errTooManyRedirects
			}
			if request.URL.Scheme != "http" && request.URL.Scheme != "https" {
				return errUnsupportedRedirect
			}
			if request.URL.User != nil {
				return errUnsupportedRedirect
			}
			return nil
		},
	}
	response, err := client.Do(request)
	checkedAt := p.now().UTC()
	latency := nonNegativeMilliseconds(checkedAt.Sub(startedAt))
	if err != nil {
		return store.UptimeProbeResult{LatencyMS: latency, Error: classifyProbeError(err), CheckedAt: checkedAt}
	}
	defer response.Body.Close()
	result := store.UptimeProbeResult{LatencyMS: latency, StatusCode: response.StatusCode, CheckedAt: checkedAt}
	if response.TLS != nil && len(response.TLS.PeerCertificates) > 0 {
		expiresAt := response.TLS.PeerCertificates[0].NotAfter.UTC()
		result.TLSChecked = true
		result.TLSExpiresAt = &expiresAt
		result.TLSExpiring = !expiresAt.After(checkedAt.Add(time.Duration(monitor.TLSExpiryThresholdDays) * 24 * time.Hour))
	}
	if response.StatusCode < monitor.ExpectedStatusMin || response.StatusCode > monitor.ExpectedStatusMax {
		result.Error = fmt.Sprintf("HTTP 状态码 %d 不在 %d-%d 范围", response.StatusCode, monitor.ExpectedStatusMin, monitor.ExpectedStatusMax)
		return result
	}
	result.Success = true
	return result
}

func (p *NetworkProber) probeTCP(ctx context.Context, monitor store.UptimeMonitor) store.UptimeProbeResult {
	startedAt := p.now()
	connection, err := p.dialContext()(ctx, "tcp", monitor.Target)
	checkedAt := p.now().UTC()
	latency := nonNegativeMilliseconds(checkedAt.Sub(startedAt))
	if err != nil {
		return store.UptimeProbeResult{LatencyMS: latency, Error: classifyProbeError(err), CheckedAt: checkedAt}
	}
	_ = connection.Close()
	return store.UptimeProbeResult{Success: true, LatencyMS: latency, CheckedAt: checkedAt}
}

func (p *NetworkProber) httpTransport(timeout time.Duration) http.RoundTripper {
	if p.Transport != nil {
		return p.Transport
	}
	dialer := &net.Dialer{Timeout: timeout}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = dialer.DialContext
	transport.TLSHandshakeTimeout = timeout
	transport.ResponseHeaderTimeout = timeout
	transport.DisableKeepAlives = true
	return transport
}

func (p *NetworkProber) dialContext() DialContextFunc {
	if p.DialContext != nil {
		return p.DialContext
	}
	return (&net.Dialer{}).DialContext
}

func (p *NetworkProber) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

func classifyProbeError(err error) string {
	if errors.Is(err, errTooManyRedirects) {
		return "HTTP 重定向次数超过 5 次"
	}
	if errors.Is(err, errUnsupportedRedirect) {
		return "HTTP 重定向目标不受支持"
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "连接超时"
	}
	var dnsError *net.DNSError
	if errors.As(err, &dnsError) {
		return "DNS 解析失败"
	}
	var certificateError x509.UnknownAuthorityError
	if errors.As(err, &certificateError) {
		return "TLS 证书验证失败"
	}
	var hostnameError x509.HostnameError
	if errors.As(err, &hostnameError) {
		return "TLS 证书主机名不匹配"
	}
	var invalidCertificate x509.CertificateInvalidError
	if errors.As(err, &invalidCertificate) {
		return "TLS 证书无效或已过期"
	}
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		return "连接超时"
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "connection refused") {
		return "连接被拒绝"
	}
	if strings.Contains(message, "network is unreachable") || strings.Contains(message, "no route to host") {
		return "目标网络不可达"
	}
	var urlError *url.Error
	if errors.As(err, &urlError) {
		return "HTTP 请求失败"
	}
	return "连接失败"
}

func nonNegativeMilliseconds(duration time.Duration) int64 {
	if duration <= 0 {
		return 0
	}
	return duration.Milliseconds()
}
