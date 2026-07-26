package uptime

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

type probeRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn probeRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

type trackingBody struct {
	read   bool
	closed bool
}

func (body *trackingBody) Read(_ []byte) (int, error) {
	body.read = true
	return 0, io.EOF
}

func (body *trackingBody) Close() error {
	body.closed = true
	return nil
}

type trackingConn struct {
	closed bool
}

func (connection *trackingConn) Read(_ []byte) (int, error)         { return 0, io.EOF }
func (connection *trackingConn) Write(value []byte) (int, error)    { return len(value), nil }
func (connection *trackingConn) Close() error                       { connection.closed = true; return nil }
func (connection *trackingConn) LocalAddr() net.Addr                { return testAddr("local") }
func (connection *trackingConn) RemoteAddr() net.Addr               { return testAddr("remote") }
func (connection *trackingConn) SetDeadline(_ time.Time) error      { return nil }
func (connection *trackingConn) SetReadDeadline(_ time.Time) error  { return nil }
func (connection *trackingConn) SetWriteDeadline(_ time.Time) error { return nil }

type testAddr string

func (address testAddr) Network() string { return "tcp" }
func (address testAddr) String() string  { return string(address) }

type timeoutError struct{}

func (timeoutError) Error() string   { return "sensitive timeout details" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

func steppedClock(values ...time.Time) func() time.Time {
	index := 0
	return func() time.Time {
		if index >= len(values) {
			return values[len(values)-1]
		}
		value := values[index]
		index++
		return value
	}
}

func TestHTTPProbeUsesGETRecordsTLSAndDoesNotReadBody(t *testing.T) {
	startedAt := time.Date(2026, 7, 25, 8, 0, 0, 0, time.UTC)
	checkedAt := startedAt.Add(42 * time.Millisecond)
	expiresAt := checkedAt.Add(10 * 24 * time.Hour)
	body := &trackingBody{}
	prober := &NetworkProber{
		Now: steppedClock(startedAt, checkedAt),
		Transport: probeRoundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.Method != http.MethodGet || request.Body != nil {
				t.Fatalf("request = %s body=%v, want bodyless GET", request.Method, request.Body)
			}
			return &http.Response{
				StatusCode: http.StatusNoContent,
				Header:     make(http.Header),
				Body:       body,
				Request:    request,
				TLS:        &tls.ConnectionState{PeerCertificates: []*x509.Certificate{{NotAfter: expiresAt}}},
			}, nil
		}),
	}
	monitor := validHTTPMonitor()
	result := prober.Probe(context.Background(), monitor)

	if !result.Success || result.StatusCode != http.StatusNoContent || result.LatencyMS != 42 {
		t.Fatalf("result = %+v", result)
	}
	if !result.TLSChecked || result.TLSExpiresAt == nil || !result.TLSExpiresAt.Equal(expiresAt) || !result.TLSExpiring {
		t.Fatalf("TLS result = %+v", result)
	}
	if body.read || !body.closed {
		t.Fatalf("body read=%v closed=%v, want close without read", body.read, body.closed)
	}
}

func TestHTTPProbeRejectsUnexpectedStatus(t *testing.T) {
	body := &trackingBody{}
	prober := &NetworkProber{
		Now: steppedClock(time.Unix(0, 0), time.Unix(0, 0)),
		Transport: probeRoundTripFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusInternalServerError, Header: make(http.Header), Body: body, Request: request}, nil
		}),
	}
	result := prober.Probe(context.Background(), validHTTPMonitor())
	if result.Success || result.StatusCode != 500 || !strings.Contains(result.Error, "500") {
		t.Fatalf("result = %+v", result)
	}
	if body.read || !body.closed {
		t.Fatalf("body read=%v closed=%v", body.read, body.closed)
	}
}

func TestHTTPProbeAllowsFiveRedirectsAndRejectsSixth(t *testing.T) {
	t.Run("allows five", func(t *testing.T) {
		requests := 0
		prober := &NetworkProber{Transport: redirectTransport(t, &requests, 5)}
		result := prober.Probe(context.Background(), validHTTPMonitor())
		if !result.Success || requests != 6 {
			t.Fatalf("result=%+v requests=%d, want initial plus five redirects", result, requests)
		}
	})

	t.Run("rejects sixth", func(t *testing.T) {
		requests := 0
		prober := &NetworkProber{Transport: redirectTransport(t, &requests, 6)}
		result := prober.Probe(context.Background(), validHTTPMonitor())
		if result.Success || result.Error != "HTTP 重定向次数超过 5 次" || requests != 6 {
			t.Fatalf("result=%+v requests=%d", result, requests)
		}
	})
}

func redirectTransport(t *testing.T, requests *int, redirects int) http.RoundTripper {
	t.Helper()
	return probeRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		*requests++
		step := *requests - 1
		response := &http.Response{Header: make(http.Header), Body: io.NopCloser(strings.NewReader("ignored")), Request: request}
		if step < redirects {
			response.StatusCode = http.StatusFound
			response.Header.Set("Location", "https://example.com/redirect/"+time.Unix(int64(step), 0).Format("150405"))
		} else {
			response.StatusCode = http.StatusOK
		}
		return response, nil
	})
}

func TestHTTPProbeClassifiesTLSValidationErrorWithoutRawDetails(t *testing.T) {
	prober := &NetworkProber{Transport: probeRoundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return nil, x509.UnknownAuthorityError{}
	})}
	result := prober.Probe(context.Background(), validHTTPMonitor())
	if result.Success || result.Error != "TLS 证书验证失败" {
		t.Fatalf("result = %+v", result)
	}
}

func TestTCPProbeConnectsAndClosesImmediately(t *testing.T) {
	startedAt := time.Date(2026, 7, 25, 8, 0, 0, 0, time.UTC)
	connection := &trackingConn{}
	prober := &NetworkProber{
		Now: steppedClock(startedAt, startedAt.Add(9*time.Millisecond)),
		DialContext: func(_ context.Context, network string, address string) (net.Conn, error) {
			if network != "tcp" || address != "example.com:443" {
				t.Fatalf("dial %s %s", network, address)
			}
			return connection, nil
		},
	}
	monitor := validHTTPMonitor()
	monitor.Type = MonitorTypeTCP
	monitor.Target = "example.com:443"
	result := prober.Probe(context.Background(), monitor)
	if !result.Success || result.LatencyMS != 9 || !connection.closed {
		t.Fatalf("result=%+v closed=%v", result, connection.closed)
	}
}

func TestTCPProbeClassifiesDNSAndTimeoutErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "DNS", err: &net.DNSError{Err: "sensitive resolver output", Name: "secret.internal"}, want: "DNS 解析失败"},
		{name: "timeout", err: timeoutError{}, want: "连接超时"},
		{name: "refused", err: errors.New("dial tcp 10.0.0.1:443: connection refused token=secret"), want: "连接被拒绝"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			prober := &NetworkProber{DialContext: func(context.Context, string, string) (net.Conn, error) { return nil, test.err }}
			monitor := validHTTPMonitor()
			monitor.Type = MonitorTypeTCP
			monitor.Target = "example.com:443"
			result := prober.Probe(context.Background(), monitor)
			if result.Error != test.want || strings.Contains(result.Error, "secret") {
				t.Fatalf("error = %q, want %q", result.Error, test.want)
			}
		})
	}
}

func TestProbeRejectsInvalidConfigurationBeforeNetwork(t *testing.T) {
	called := false
	prober := &NetworkProber{Transport: probeRoundTripFunc(func(_ *http.Request) (*http.Response, error) {
		called = true
		return nil, errors.New("should not run")
	})}
	monitor := validHTTPMonitor()
	monitor.Target = "not a URL"
	result := prober.Probe(context.Background(), monitor)
	if called || result.Error != "拨测配置无效" || result.CheckedAt.IsZero() {
		t.Fatalf("called=%v result=%+v", called, result)
	}
}
