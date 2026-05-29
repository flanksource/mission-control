package tunnel

import (
	"context"
	"net"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// NewTransport returns an HTTP transport that sends requests to the agent over
// the yamux tunnel and signs each request with a short-lived upstream JWT.
func NewTransport(agentID uuid.UUID) http.RoundTripper {
	return AuthenticatedTransport(&http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return Open(ctx, agentID)
		},
		ForceAttemptHTTP2:     false,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
	})
}

// AuthenticatedTransport wraps an HTTP transport and adds upstream tunnel auth
// per request. It is safe with keep-alives because the token is injected at the
// HTTP request layer instead of by mutating connection bytes.
func AuthenticatedTransport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return upstreamAuthTransport{base: base}
}

type upstreamAuthTransport struct {
	base http.RoundTripper
}

func (t upstreamAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	token, err := mintUpstreamToken()
	if err != nil {
		return nil, err
	}

	clone := req.Clone(req.Context())
	clone.Header = req.Header.Clone()
	if clone.Header == nil {
		clone.Header = make(http.Header)
	}
	clone.Header.Del(UpstreamAuthHeader)
	clone.Header.Set(UpstreamAuthHeader, token)

	return t.base.RoundTrip(clone)
}
