package sdk

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"math/rand/v2"
	"net"
	stdhttp "net/http"
	"strings"
	"syscall"
	"time"

	"github.com/flanksource/commons/logger"
)

// RetryPolicy bounds how often a request is replayed. Zero retries disables it.
//
// This exists because a Mission Control command is often many round trips — an access export pages
// through every user, and a catalog read runs one request per config item — so a single
// `dial tcp …: i/o timeout` part way through discards every result already fetched.
type RetryPolicy struct {
	// Retries is the number of ADDITIONAL attempts after the first, matching `curl --retry`.
	Retries int
	// Delay is a fixed wait between attempts, jittered. Not an exponential base.
	Delay time.Duration
}

func (p RetryPolicy) enabled() bool { return p.Retries > 0 }

// WithRetry replays idempotent requests that fail transiently. retries counts attempts after the
// first, so 3 means at most 4; 0 disables retry entirely.
func WithRetry(retries int, delay time.Duration) ClientOption {
	return func(c *Client) {
		c.retry = RetryPolicy{Retries: retries, Delay: delay}
	}
}

/*
Requests whose method says "write" but whose semantics are a read: both carry their query in the
body only because it is too large for a URL, and neither changes server state. Without this list
they would be treated as writes, which would leave the paths behind `catalog list`, `catalog
search`, `catalog changes`, the projection queries, and the id-resolution prelude of every access
command with no retry at all — that is most of what faro does.

Matched exactly rather than by prefix, so no plugin invoke or playbook route can ever fall in.
*/
var replayableReadPosts = map[string]bool{
	"/resources/search": true,
	"/catalog/changes":  true,
}

// Statuses worth replaying: all three are emitted by an ingress or load balancer that could not
// reach a healthy backend, so they carry no application meaning and say nothing about whether
// asking again will fail the same way.
//
// 500 is deliberately absent and should stay absent. Today it is what an unknown catalog id
// returns, so retrying would spend the whole budget re-asking for a record that does not exist;
// once that becomes a 404 it will mean an unhandled server error, which repeats deterministically.
// Neither reading is worth a replay. 429 is absent because a rate limit is a 4xx.
var replayableStatuses = map[int]bool{
	stdhttp.StatusBadGateway:         true,
	stdhttp.StatusServiceUnavailable: true,
	stdhttp.StatusGatewayTimeout:     true,
}

// replayable reports whether sending this request twice is harmless.
func replayable(method, path string) bool {
	switch method {
	case stdhttp.MethodGet, stdhttp.MethodHead:
		return true
	case stdhttp.MethodPost:
		return replayableReadPosts[normalizeAPIPath(path)]
	default:
		return false
	}
}

// normalizeAPIPath drops the /api mount so one route list covers both server URL shapes: the
// client's base URL may already end in /api, which decides whether the mount appears in the path.
func normalizeAPIPath(path string) string {
	if trimmed := strings.TrimPrefix(path, "/api"); strings.HasPrefix(trimmed, "/") {
		return trimmed
	}
	return path
}

/*
shouldRetry decides on the failure stage first and the method second.

A request that never reached the server can be replayed whatever its method: the connection was
never established, so no playbook ran and no plugin was invoked. That is the case this whole
change exists for — `dial tcp …: i/o timeout`.

Once bytes may have been written the method starts to matter, because a reset or an EOF cannot
distinguish "the server never saw it" from "the server did the work and the answer was lost".
Replaying a playbook run there would start a second one, so only replayable requests continue.
*/
func shouldRetry(method, path string, status int, err error) bool {
	if err != nil {
		// Only a failure recognisably from the network is replayed. Anything else reaching here
		// came from a middleware rather than the wire — a rejected refresh token is the one that
		// matters, since it is permanent and asking again only hammers the auth endpoint.
		if !networkFailure(err) {
			return false
		}
		if neverSent(err) {
			return true
		}
		return replayable(method, path)
	}
	return replayableStatuses[status] && replayable(method, path)
}

// networkFailure reports whether err is a transport-level failure that another attempt could
// plausibly get past. Unrecognised errors are deliberately not retried: an error this cannot name
// is one it cannot reason about, and replaying it hides the real message behind a delay.
func networkFailure(err error) bool {
	if permanentTransportFailure(err) {
		return false
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	// A server that closed the connection mid-response surfaces as one of these rather than as a
	// net.Error, and it is the shape a dropped Mission Control connection most often takes.
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	return errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.EPIPE)
}

// neverSent reports whether the connection itself was never established, which is the one case
// where a replay cannot duplicate a side effect. Go labels that stage "dial".
func neverSent(err error) bool {
	var opErr *net.OpError
	return errors.As(err, &opErr) && opErr.Op == "dial"
}

// permanentTransportFailure covers the failures that will repeat identically, where a retry only
// delays the message the caller needs to read.
func permanentTransportFailure(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var certErr *tls.CertificateVerificationError
	var unknownAuthority x509.UnknownAuthorityError
	var hostnameErr x509.HostnameError
	return errors.As(err, &certErr) || errors.As(err, &unknownAuthority) || errors.As(err, &hostnameErr)
}

func retryMiddleware(policy RetryPolicy) func(stdhttp.RoundTripper) stdhttp.RoundTripper {
	return func(next stdhttp.RoundTripper) stdhttp.RoundTripper {
		return roundTripperFunc(func(req *stdhttp.Request) (*stdhttp.Response, error) {
			// A body that could not be buffered was streamed once and is already drained; replaying
			// it would send an empty body, which reads as a malformed request rather than a retry.
			if req.Body != nil && req.GetBody == nil {
				return next.RoundTrip(req)
			}
			for attempt := 0; ; attempt++ {
				response, err := next.RoundTrip(req)
				status := 0
				if response != nil {
					status = response.StatusCode
				}
				if attempt >= policy.Retries || !shouldRetry(req.Method, req.URL.Path, status, err) {
					return response, err
				}
				if response != nil {
					// Drain a bounded prefix before discarding so the connection returns to the
					// pool; an unread body pins one per attempt.
					_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64*1024))
					_ = response.Body.Close()
				}
				// Warn, not debug: retries turn one slow failure into several, and a run that
				// silently took four times as long is the report nobody can act on.
				logger.Warnf("retrying %s %s after %s (attempt %d of %d)",
					req.Method, req.URL.Path, retryReason(status, err), attempt+2, policy.Retries+1)
				select {
				case <-req.Context().Done():
					return nil, req.Context().Err()
				case <-time.After(jitter(policy.Delay)):
				}
				if req.GetBody != nil {
					body, bodyErr := req.GetBody()
					if bodyErr != nil {
						return nil, bodyErr
					}
					req.Body = body
				}
			}
		})
	}
}

func retryReason(status int, err error) string {
	if err != nil {
		return err.Error()
	}
	return stdhttp.StatusText(status)
}

// jitter spreads the wait by ±20%. Catalog reads run four batches at once, so an exact delay would
// re-fire all four together at the moment a recovering server can least absorb them.
func jitter(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	return time.Duration(float64(d) * (0.8 + 0.4*rand.Float64()))
}
