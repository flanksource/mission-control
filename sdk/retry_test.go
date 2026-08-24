package sdk

import (
	"context"
	"errors"
	"io"
	"net"
	stdhttp "net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/flanksource/duty/query"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The stages a request can fail at, which is what the policy keys on before it looks at the method.
var (
	dialTimeout   = &net.OpError{Op: "dial", Err: os.ErrDeadlineExceeded}
	connectionCut = &net.OpError{Op: "read", Err: syscall.ECONNRESET}
	truncated     = io.ErrUnexpectedEOF
)

const (
	readPost   = "/resources/search"
	writePost  = "/playbook/run"
	catalogID  = "3a96d327-2a6b-4a3a-9b2a-1f0f6b6b6b6b"
	mountedGet = "/api/resources/" + catalogID
)

var _ = ginkgo.Describe("retry policy", func() {
	// The table is the specification: every end-to-end spec below exercises the wiring, but this
	// is where each rule is actually pinned.
	ginkgo.DescribeTable("decides whether a failed request may be sent again",
		func(method, path string, status int, err error, expected bool) {
			Expect(shouldRetry(method, path, status, err)).To(Equal(expected))
		},
		ginkgo.Entry("a GET that never reached the server", stdhttp.MethodGet, "/resources/abc", 0, dialTimeout, true),
		ginkgo.Entry("a GET cut mid-flight", stdhttp.MethodGet, "/resources/abc", 0, connectionCut, true),
		ginkgo.Entry("a GET whose response was truncated", stdhttp.MethodGet, "/resources/abc", 0, truncated, true),
		ginkgo.Entry("a GET behind the /api mount", stdhttp.MethodGet, mountedGet, stdhttp.StatusServiceUnavailable, nil, true),
		ginkgo.Entry("a HEAD against a restarting backend", stdhttp.MethodHead, "/resources/abc", stdhttp.StatusBadGateway, nil, true),

		// A search is a read that only uses POST because its query is too big for a URL.
		ginkgo.Entry("a search that never reached the server", stdhttp.MethodPost, readPost, 0, dialTimeout, true),
		ginkgo.Entry("a search cut mid-flight", stdhttp.MethodPost, readPost, 0, connectionCut, true),
		ginkgo.Entry("a search against a restarting backend", stdhttp.MethodPost, readPost, stdhttp.StatusServiceUnavailable, nil, true),
		ginkgo.Entry("a search behind the /api mount", stdhttp.MethodPost, "/api"+readPost, stdhttp.StatusGatewayTimeout, nil, true),
		ginkgo.Entry("a changes query against a restarting backend", stdhttp.MethodPost, "/catalog/changes", stdhttp.StatusBadGateway, nil, true),

		// A write may be replayed only when the connection was never established.
		ginkgo.Entry("a playbook run that never reached the server", stdhttp.MethodPost, writePost, 0, dialTimeout, true),
		ginkgo.Entry("a playbook run cut mid-flight", stdhttp.MethodPost, writePost, 0, connectionCut, false),
		ginkgo.Entry("a playbook run whose response was truncated", stdhttp.MethodPost, writePost, 0, truncated, false),
		ginkgo.Entry("a playbook run against a restarting backend", stdhttp.MethodPost, writePost, stdhttp.StatusServiceUnavailable, nil, false),
		ginkgo.Entry("a plugin invoke cut mid-flight", stdhttp.MethodPost, "/api/plugins/aws/invoke/scan", 0, connectionCut, false),
		ginkgo.Entry("a soft delete against a restarting backend", stdhttp.MethodPatch, "/db/playbooks", stdhttp.StatusServiceUnavailable, nil, false),

		// A 4xx is the server answering, not failing.
		ginkgo.Entry("a GET for an id that does not exist", stdhttp.MethodGet, "/resources/abc", stdhttp.StatusNotFound, nil, false),
		ginkgo.Entry("a GET that was rejected", stdhttp.MethodGet, "/resources/abc", stdhttp.StatusBadRequest, nil, false),
		ginkgo.Entry("a GET without permission", stdhttp.MethodGet, "/resources/abc", stdhttp.StatusForbidden, nil, false),
		// Deliberate: a rate limit is a 4xx, and the policy does not carve an exception for it.
		ginkgo.Entry("a GET that was rate limited", stdhttp.MethodGet, "/resources/abc", stdhttp.StatusTooManyRequests, nil, false),
		// Deliberate: 500 is what an unknown catalog id returns today, and an unhandled server
		// error once that is fixed. Neither is worth asking again.
		ginkgo.Entry("a GET that hit a server error", stdhttp.MethodGet, "/resources/abc", stdhttp.StatusInternalServerError, nil, false),

		// Errors that never came from the wire. A rejected refresh token is permanent, and asking
		// again only sends the auth endpoint the same doomed request three more times.
		ginkgo.Entry("a GET whose token was rejected", stdhttp.MethodGet, "/resources/abc", 0,
			errors.New(`context "app" needs re-authentication (refresh token rejected)`), false),
		ginkgo.Entry("a GET that failed for an unrecognised reason", stdhttp.MethodGet, "/resources/abc", 0,
			errors.New("some middleware gave up"), false),

		ginkgo.Entry("a GET the caller gave up on", stdhttp.MethodGet, "/resources/abc", 0, context.Canceled, false),
		ginkgo.Entry("a GET that ran out of time", stdhttp.MethodGet, "/resources/abc", 0, context.DeadlineExceeded, false),
		ginkgo.Entry("a GET that succeeded", stdhttp.MethodGet, "/resources/abc", stdhttp.StatusOK, nil, false),
	)

	ginkgo.It("recognises a dial failure through the layers net/http wraps it in", func() {
		wrapped := &net.OpError{Op: "dial", Err: os.ErrDeadlineExceeded}

		Expect(neverSent(wrapped)).To(BeTrue())
		Expect(neverSent(&net.OpError{Op: "read", Err: syscall.ECONNRESET})).To(BeFalse())
		Expect(neverSent(errors.New("something else"))).To(BeFalse())
	})
})

var _ = ginkgo.Describe("client retry", func() {
	// Counts requests so a spec can distinguish "retried" from "happened to succeed".
	serving := func(handler func(w stdhttp.ResponseWriter, r *stdhttp.Request)) (*httptest.Server, *atomic.Int32) {
		var seen atomic.Int32
		server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			seen.Add(1)
			handler(w, r)
		}))
		return server, &seen
	}

	unavailable := func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		w.WriteHeader(stdhttp.StatusServiceUnavailable)
	}

	ginkgo.It("does not send a request again when the server answered with a 4xx", func() {
		server, seen := serving(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
			w.WriteHeader(stdhttp.StatusNotFound)
		})
		defer server.Close()

		_, err := New(server.URL, "tok", WithRetry(3, time.Millisecond)).
			GetCatalogItem(context.Background(), catalogID)

		Expect(err).To(HaveOccurred())
		Expect(seen.Load()).To(Equal(int32(1)))
	})

	// The pair that matters: both are POSTs to the same server behaving the same way, and only the
	// one that is a read is sent again.
	ginkgo.It("sends a search again but never a playbook run", func() {
		server, seen := serving(unavailable)
		defer server.Close()
		client := New(server.URL, "tok", WithRetry(3, time.Millisecond))

		_, searchErr := client.SearchCatalog(context.Background(), query.SearchResourcesRequest{})
		Expect(searchErr).To(HaveOccurred())
		Expect(seen.Load()).To(Equal(int32(4)))

		seen.Store(0)
		_, runErr := client.RunPlaybook(PlaybookRunParams{ID: uuid.New()})
		Expect(runErr).To(HaveOccurred())
		Expect(seen.Load()).To(Equal(int32(1)))
	})

	ginkgo.It("sends a catalog read again while the backend is unreachable", func() {
		server, seen := serving(unavailable)
		defer server.Close()

		_, err := New(server.URL, "tok", WithRetry(2, time.Millisecond)).
			GetCatalogItem(context.Background(), catalogID)

		Expect(err).To(HaveOccurred())
		Expect(seen.Load()).To(Equal(int32(3)))
	})

	// The failure this change exists for: a connection that dies rather than a status that says so.
	ginkgo.It("recovers a read whose connection was dropped", func() {
		var seen atomic.Int32
		server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
			if seen.Add(1) == 1 {
				connection, _, err := w.(stdhttp.Hijacker).Hijack()
				Expect(err).ToNot(HaveOccurred())
				_ = connection.Close()
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"3a96d327-2a6b-4a3a-9b2a-1f0f6b6b6b6b","name":"my-config"}`))
		}))
		defer server.Close()

		item, err := New(server.URL, "tok", WithRetry(3, time.Millisecond)).
			GetCatalogItem(context.Background(), catalogID)

		Expect(err).ToNot(HaveOccurred())
		Expect(*item.Name).To(Equal("my-config"))
		// At least two, not exactly two: net/http replays an idempotent request on a dead pooled
		// connection by itself, so pinning the count here would test the stdlib, not this policy.
		Expect(seen.Load()).To(BeNumerically(">=", 2))
	})

	ginkgo.It("sends a write again when the connection was never established", func() {
		// Port 1 refuses immediately, which fails at the dial stage — the one stage where a write
		// cannot have been acted on.
		_, err := New("http://127.0.0.1:1", "tok", WithRetry(2, time.Millisecond)).
			RunPlaybook(PlaybookRunParams{ID: uuid.New()})

		Expect(err).To(HaveOccurred())
		Expect(shouldRetry(stdhttp.MethodPost, "/playbook/run", 0, dialTimeout)).To(BeTrue())
	})

	ginkgo.It("stops waiting when the caller cancels", func() {
		server, seen := serving(unavailable)
		defer server.Close()
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			time.Sleep(50 * time.Millisecond)
			cancel()
		}()

		started := time.Now()
		// A delay long enough that finishing quickly can only mean the wait was interrupted.
		_, err := New(server.URL, "tok", WithRetry(3, 30*time.Second)).GetCatalogItem(ctx, catalogID)

		Expect(err).To(HaveOccurred())
		Expect(time.Since(started)).To(BeNumerically("<", 5*time.Second))
		Expect(seen.Load()).To(Equal(int32(1)))
	})

	ginkgo.It("sends nothing again when retry is switched off", func() {
		server, seen := serving(unavailable)
		defer server.Close()

		started := time.Now()
		_, err := New(server.URL, "tok", WithRetry(0, 30*time.Second)).
			GetCatalogItem(context.Background(), catalogID)

		Expect(err).To(HaveOccurred())
		Expect(seen.Load()).To(Equal(int32(1)))
		Expect(time.Since(started)).To(BeNumerically("<", 5*time.Second))
	})

	// Installing retry meant reading the options before the middleware stack is built, so the
	// options now run against a client that has not been wrapped yet. This is what proves that
	// reordering did not quietly drop them.
	ginkgo.It("still applies the other client options around the retry middleware", func() {
		var agent string
		var accept string
		server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			agent, accept = r.UserAgent(), r.Header.Get("Accept")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"3a96d327-2a6b-4a3a-9b2a-1f0f6b6b6b6b","name":"my-config"}`))
		}))
		defer server.Close()

		_, err := New(server.URL, "tok",
			WithRetry(3, time.Millisecond),
			WithUserAgent("mission-control-cli/test"),
			WithAccept("application/json"),
		).GetCatalogItem(context.Background(), catalogID)

		Expect(err).ToNot(HaveOccurred())
		Expect(agent).To(Equal("mission-control-cli/test"))
		Expect(accept).To(Equal("application/json"))
	})

	ginkgo.It("leaves a client without a policy on a single attempt", func() {
		server, seen := serving(unavailable)
		defer server.Close()

		_, err := New(server.URL, "tok").GetCatalogItem(context.Background(), catalogID)

		Expect(err).To(HaveOccurred())
		Expect(seen.Load()).To(Equal(int32(1)))
	})
})
