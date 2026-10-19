package harness

import (
	"net/http"
	"net/http/httptest"
	"sync"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Fixture creates what a case's requests need, as the admin, in the case's namespace, and deletes it after the case.
type Fixture struct {
	Namespace string

	cleanups []func()
}

func newFixture(namespace string) *Fixture {
	return &Fixture{Namespace: namespace}
}

// Admin makes requests as the admin, to set up a request or check what it did.
func (f *Fixture) Admin() *Caller {
	return env.server.Admin
}

// Defer runs fn after the case.
func (f *Fixture) Defer(fn func()) {
	f.cleanups = append(f.cleanups, fn)
}

func (f *Fixture) cleanup() {
	for i := len(f.cleanups) - 1; i >= 0; i-- {
		f.cleanups[i]()
	}
	f.cleanups = nil
}

// Name returns a name of the prefix, unique within the case.
func (f *Fixture) Name(prefix string) string {
	return prefix + "-" + randomSuffix()
}

// Secret returns a credential that's unique to one fixture, so a response can be searched for it.
func (f *Fixture) Secret() string {
	return "authz-secret-" + randomSuffix() + randomSuffix()
}

// Sink is an HTTP server that records the requests Mission Control makes to it.
type Sink struct {
	URL string

	mu       sync.Mutex
	requests []*http.Request
}

// Sink starts an HTTP server for Mission Control to call, e.g. through a connection.
func (f *Fixture) Sink() *Sink {
	sink := &Sink{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sink.mu.Lock()
		sink.requests = append(sink.requests, r.Clone(r.Context()))
		sink.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	sink.URL = server.URL
	f.Defer(server.Close)
	return sink
}

// Calls returns the requests made to the sink so far.
func (s *Sink) Calls() []*http.Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*http.Request(nil), s.requests...)
}

// mustDo makes the request as the admin and expects one of the statuses.
func (f *Fixture) mustDo(r Request, statuses ...int) Response {
	ginkgo.GinkgoHelper()
	resp := f.Admin().Do(r)
	Expect(resp.Status).To(BeElementOf(statuses), resp.String())
	return resp
}
