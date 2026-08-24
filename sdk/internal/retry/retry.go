package retry

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

type Policy struct {
	Retries int
	Delay   time.Duration
}

var replayableReadPosts = map[string]bool{
	"/resources/search": true,
	"/catalog/changes":  true,
}

var replayableStatuses = map[int]bool{
	stdhttp.StatusBadGateway:         true,
	stdhttp.StatusServiceUnavailable: true,
	stdhttp.StatusGatewayTimeout:     true,
}

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

func normalizeAPIPath(path string) string {
	if trimmed := strings.TrimPrefix(path, "/api"); strings.HasPrefix(trimmed, "/") {
		return trimmed
	}
	return path
}

func shouldRetry(method, path string, status int, err error) bool {
	if err == nil {
		return replayableStatuses[status] && replayable(method, path)
	}
	if !networkFailure(err) {
		return false
	}
	return neverSent(err) || replayable(method, path)
}

func ShouldRetry(method, path string, status int, err error) bool {
	return shouldRetry(method, path, status, err)
}

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
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.EPIPE)
}

func neverSent(err error) bool {
	var opErr *net.OpError
	return errors.As(err, &opErr) && opErr.Op == "dial"
}

func NeverSent(err error) bool {
	return neverSent(err)
}

func permanentTransportFailure(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var certErr *tls.CertificateVerificationError
	var unknownAuthority x509.UnknownAuthorityError
	var hostnameErr x509.HostnameError
	return errors.As(err, &certErr) || errors.As(err, &unknownAuthority) || errors.As(err, &hostnameErr)
}

func Middleware(policy Policy) func(stdhttp.RoundTripper) stdhttp.RoundTripper {
	return func(next stdhttp.RoundTripper) stdhttp.RoundTripper {
		return roundTripperFunc(func(req *stdhttp.Request) (*stdhttp.Response, error) {
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
					_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64*1024))
					_ = response.Body.Close()
				}
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

func jitter(delay time.Duration) time.Duration {
	if delay <= 0 {
		return 0
	}
	return time.Duration(float64(delay) * (0.8 + 0.4*rand.Float64()))
}

type roundTripperFunc func(*stdhttp.Request) (*stdhttp.Response, error)

func (f roundTripperFunc) RoundTrip(req *stdhttp.Request) (*stdhttp.Response, error) {
	return f(req)
}
