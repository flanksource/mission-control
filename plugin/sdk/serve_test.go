package sdk

import (
	"bufio"
	"net"
	"net/http"
	"net/http/httptest"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("bufferedResponse", func() {
	ginkgo.It("supports websocket hijacking through ResponseController", func() {
		target := newHijackableResponse()
		buf := &bufferedResponse{header: http.Header{"X-Test": []string{"yes"}}, target: target}

		conn, brw, err := http.NewResponseController(buf).Hijack()
		Expect(err).ToNot(HaveOccurred())
		defer conn.Close()
		defer target.closePeer()

		Expect(brw).ToNot(BeNil())
		Expect(target.hijacked).To(BeTrue())
		Expect(buf.committed).To(BeTrue())
		Expect(buf.Header()).To(Equal(target.Header()))
	})

	ginkgo.It("does not fall through to static assets after hijack", func() {
		target := newHijackableResponse()
		defer target.closePeer()

		staticCalled := false
		plugin := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			conn, _, err := http.NewResponseController(w).Hijack()
			Expect(err).ToNot(HaveOccurred())
			_ = conn.Close()
		})
		static := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			staticCalled = true
		})

		composeHandler(plugin, static).ServeHTTP(target, httptest.NewRequest(http.MethodGet, "/ws", nil))

		Expect(staticCalled).To(BeFalse())
	})
})

type hijackableResponse struct {
	header   http.Header
	status   int
	hijacked bool
	peer     net.Conn
}

func newHijackableResponse() *hijackableResponse {
	return &hijackableResponse{header: http.Header{}}
}

func (h *hijackableResponse) Header() http.Header {
	return h.header
}

func (h *hijackableResponse) Write(p []byte) (int, error) {
	return len(p), nil
}

func (h *hijackableResponse) WriteHeader(status int) {
	h.status = status
}

func (h *hijackableResponse) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h.hijacked = true
	client, peer := net.Pipe()
	h.peer = peer
	return client, bufio.NewReadWriter(bufio.NewReader(client), bufio.NewWriter(client)), nil
}

func (h *hijackableResponse) closePeer() {
	if h.peer != nil {
		_ = h.peer.Close()
	}
}
