package tunnel

import (
	"bufio"
	gocontext "context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/flanksource/duty/api"
	dutyContext "github.com/flanksource/duty/context"
	"github.com/flanksource/duty/upstream"
	"github.com/hashicorp/yamux"
	"github.com/sethvargo/go-retry"
)

func hasHeaderToken(value, token string) bool {
	for part := range strings.SplitSeq(value, ",") {
		if strings.EqualFold(strings.TrimSpace(part), token) {
			return true
		}
	}
	return false
}

// tunnelAddr is a minimal net.Addr implementation required by net.Listener.
// The HTTP server only uses it for logging/metadata; yamux streams do not have
// a meaningful local network address.
type tunnelAddr string

func (a tunnelAddr) Network() string { return string(a) }
func (a tunnelAddr) String() string  { return string(a) }

// yamuxListener adapts a yamux session to net.Listener so http.Server can serve
// normal HTTP requests over each yamux stream opened by upstream.
type yamuxListener struct {
	session *yamux.Session
}

func (l yamuxListener) Accept() (net.Conn, error) {
	return l.session.Accept()
}

func (l yamuxListener) Close() error {
	return l.session.Close()
}

func (l yamuxListener) Addr() net.Addr {
	return tunnelAddr("yamux-agent-tunnel")
}

func StartAgentTunnel(ctx dutyContext.Context, upstreamConfig upstream.UpstreamConfig, handler http.Handler) {
	backoff := retry.NewConstant(2 * time.Second)
	_ = retry.Do(ctx, backoff, func(retryCtx gocontext.Context) error {
		if err := runAgentTunnelSession(retryCtx, upstreamConfig, handler); err != nil {
			if errors.Is(err, gocontext.Canceled) || retryCtx.Err() != nil {
				return retryCtx.Err()
			}
			ctx.Warnf("agent tunnel disconnected: %v", err)
		}
		return retry.RetryableError(fmt.Errorf("agent tunnel disconnected"))
	})
}

// runAgentTunnelSession connects this agent to upstream and serves the agent's
// normal HTTP handler over the resulting yamux session.
//
// Upstream opens one yamux stream per HTTP request. On the agent side each
// stream is accepted by yamuxListener and handed to http.Server, so requests are
// routed through the same Echo handler stack as local HTTP traffic.
//
// The tunnel handler verifies the upstream-signed JWT on each request before marking the
// request as trusted upstream traffic.
func runAgentTunnelSession(ctx gocontext.Context, upstreamConfig upstream.UpstreamConfig, handler http.Handler) error {
	conn, err := dialUpgrade(ctx, upstreamConfig, fmt.Sprintf("/upstream/%s", SessionCreateHTTPEndpoint))
	if err != nil {
		return err
	}

	session, err := yamux.Client(conn, nil)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("start yamux client: %w", err)
	}
	defer session.Close()

	done := make(chan struct{})
	defer close(done)

	go func() {
		select {
		case <-ctx.Done():
			_ = session.Close()
		case <-done:
		}
	}()

	server := &http.Server{
		Handler:           authenticatedUpstreamHandler(upstreamConfig, handler),
		ReadHeaderTimeout: 30 * time.Second,
	}

	err = server.Serve(yamuxListener{session: session})
	if err == nil || errors.Is(err, http.ErrServerClosed) || errors.Is(err, net.ErrClosed) || errors.Is(err, yamux.ErrSessionShutdown) {
		return nil
	}
	return err
}

func dialUpgrade(ctx gocontext.Context, upstreamConfig upstream.UpstreamConfig, connectPath string) (net.Conn, error) {
	u, err := tunnelURL(upstreamConfig, connectPath)
	if err != nil {
		return nil, api.Errorf(api.EINVALID, "failed to create upstream yamux endpoint: %v", err)
	}

	conn, err := dialTunnelConn(ctx, u, upstreamConfig.InsecureSkipVerify)
	if err != nil {
		return nil, err
	}

	deadline := time.Now().Add(30 * time.Second)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	_ = conn.SetDeadline(deadline)

	if err := writeUpgradeRequest(conn, u, upstreamConfig); err != nil {
		_ = conn.Close()
		return nil, err
	}

	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, nil)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("read tunnel upgrade response: %w", err)
	}

	if resp.StatusCode != http.StatusSwitchingProtocols {
		var body string
		if resp.Body != nil {
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			_ = resp.Body.Close()
			body = strings.TrimSpace(string(b))
		}

		_ = conn.Close()
		if body != "" {
			return nil, fmt.Errorf("tunnel upgrade failed: HTTP %d: %s", resp.StatusCode, body)
		}

		return nil, fmt.Errorf("tunnel upgrade failed: HTTP %d", resp.StatusCode)
	}

	if !hasHeaderToken(resp.Header.Get("Connection"), "upgrade") {
		_ = conn.Close()
		return nil, fmt.Errorf("tunnel upgrade response missing Connection: Upgrade")
	}

	if !strings.EqualFold(strings.TrimSpace(resp.Header.Get("Upgrade")), "yamux") {
		_ = conn.Close()
		return nil, fmt.Errorf("tunnel upgrade response has invalid Upgrade header %q", resp.Header.Get("Upgrade"))
	}

	_ = conn.SetDeadline(time.Time{})
	return bufferedConn{Conn: conn, reader: reader}, nil
}

func tunnelURL(upstreamConfig upstream.UpstreamConfig, connectPath string) (*url.URL, error) {
	base := strings.TrimRight(upstreamConfig.Host, "/")
	if base == "" {
		return nil, fmt.Errorf("upstream host is required")
	}

	u, err := url.Parse(base + connectPath)
	if err != nil {
		return nil, fmt.Errorf("parse upstream tunnel URL: %w", err)
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("unsupported upstream scheme %q", u.Scheme)
	}

	q := u.Query()
	q.Set(upstream.AgentNameQueryParam, upstreamConfig.AgentName)
	u.RawQuery = q.Encode()
	return u, nil
}

func dialTunnelConn(ctx gocontext.Context, u *url.URL, insecureSkipVerify bool) (net.Conn, error) {
	addr := canonicalAddr(u)
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	if u.Scheme == "https" {
		tlsDialer := tls.Dialer{
			NetDialer: dialer,
			Config: &tls.Config{
				ServerName:         u.Hostname(),
				InsecureSkipVerify: insecureSkipVerify, //nolint:gosec
			},
		}

		conn, err := tlsDialer.DialContext(ctx, "tcp", addr)
		if err != nil {
			return nil, fmt.Errorf("dial upstream tunnel: %w", err)
		}
		return conn, nil
	}

	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("dial upstream tunnel: %w", err)
	}
	return conn, nil
}

func canonicalAddr(u *url.URL) string {
	if port := u.Port(); port != "" {
		return net.JoinHostPort(u.Hostname(), port)
	}
	if u.Scheme == "https" {
		return net.JoinHostPort(u.Hostname(), "443")
	}
	return net.JoinHostPort(u.Hostname(), "80")
}

func writeUpgradeRequest(conn net.Conn, u *url.URL, upstreamConfig upstream.UpstreamConfig) error {
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}

	req.Host = u.Host
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "yamux")
	if upstreamConfig.Username != "" || upstreamConfig.Password != "" {
		req.SetBasicAuth(upstreamConfig.Username, upstreamConfig.Password)
	}

	if err := req.Write(conn); err != nil {
		return fmt.Errorf("write tunnel upgrade request: %w", err)
	}

	return nil
}

type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c bufferedConn) Read(p []byte) (int, error) {
	if c.reader != nil && c.reader.Buffered() > 0 {
		return c.reader.Read(p)
	}
	return c.Conn.Read(p)
}
