package httpobservability

import (
	"bytes"
	"io"
	"net/http"
	"strings"

	"github.com/flanksource/commons/logger"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("HTTP logging levels", func() {
	ginkgo.It("maps verbosity levels to progressively richer HTTP logging", func() {
		for _, tt := range []struct {
			name  string
			level logger.LogLevel
			opts  httpLogOptions
		}{
			{name: "off", level: logger.Info, opts: httpLogOptions{}},
			{name: "summary", level: logger.Debug, opts: httpLogOptions{summary: true}},
			{name: "headers", level: logger.Trace, opts: httpLogOptions{summary: true, headers: true}},
			{name: "request body", level: logger.Trace1, opts: httpLogOptions{summary: true, headers: true, requestBody: true}},
			{name: "response body", level: logger.Trace2, opts: httpLogOptions{summary: true, headers: true, requestBody: true, responseBody: true}},
		} {
			ginkgo.By(tt.name)
			opts := httpLogOptionsFor(func(level logger.LogLevel) bool {
				return level <= tt.level
			})
			Expect(opts).To(Equal(tt.opts))
		}
	})

	ginkgo.It("prints only the detail enabled by the current HTTP logger level", func() {
		for _, tt := range []struct {
			name        string
			level       logger.LogLevel
			contains    []string
			notContains []string
		}{
			{
				name:        "summary only",
				level:       logger.Debug,
				contains:    []string{"POST", "/thing", "201"},
				notContains: []string{"X-Test", "X-Response", "request-value", "response-value"},
			},
			{
				name:        "headers",
				level:       logger.Trace,
				contains:    []string{"POST", "/thing", "201", "X-Test", "X-Response"},
				notContains: []string{"request-value", "response-value"},
			},
			{
				name:        "request body",
				level:       logger.Trace1,
				contains:    []string{"POST", "/thing", "201", "X-Test", "X-Response", "request-value"},
				notContains: []string{"response-value"},
			},
			{
				name:     "response body",
				level:    logger.Trace2,
				contains: []string{"POST", "/thing", "201", "X-Test", "X-Response", "request-value", "response-value"},
			},
		} {
			ginkgo.By(tt.name)
			var out bytes.Buffer
			restore := logger.GetOutput()
			logger.SetOutput(&out)
			func() {
				defer logger.SetOutput(restore)

				log := logger.New("http")
				log.SetLogLevel(tt.level)
				client := &http.Client{Transport: newHTTPLogRoundTripper(log, testRoundTripper(func(req *http.Request) (*http.Response, error) {
					body := `{"result":"response-value"}`
					return &http.Response{
						StatusCode:    http.StatusCreated,
						Status:        "201 Created",
						Header:        http.Header{"Content-Type": []string{"application/json"}, "X-Response": []string{"ok"}},
						Body:          io.NopCloser(strings.NewReader(body)),
						ContentLength: int64(len(body)),
						Request:       req,
					}, nil
				}))}

				req, err := http.NewRequest(http.MethodPost, "http://example.com/thing", strings.NewReader(`{"input":"request-value"}`))
				Expect(err).ToNot(HaveOccurred())
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("X-Test", "yes")

				resp, err := client.Do(req)
				Expect(err).ToNot(HaveOccurred())
				_, err = io.ReadAll(resp.Body)
				Expect(err).ToNot(HaveOccurred())
				Expect(resp.Body.Close()).To(Succeed())
			}()

			output := out.String()
			for _, expected := range tt.contains {
				Expect(output).To(ContainSubstring(expected), "output:\n%s", output)
			}
			for _, unexpected := range tt.notContains {
				Expect(output).ToNot(ContainSubstring(unexpected), "output:\n%s", output)
			}
		}
	})
})

type testRoundTripper func(*http.Request) (*http.Response, error)

func (rt testRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return rt(req)
}
