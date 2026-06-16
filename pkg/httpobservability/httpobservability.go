package httpobservability

import (
	nethttp "net/http"
	"sync"
	"time"

	"github.com/flanksource/commons/console"
	"github.com/flanksource/commons/har"
	commonshttp "github.com/flanksource/commons/http"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/commons/logger/httpretty"
	"github.com/flanksource/commons/properties"
)

var harState struct {
	sync.RWMutex
	collector *har.Collector
}

// SetHARCollector installs a process-wide HAR collector for subsequently
// created HTTP clients and returns a restore function.
func SetHARCollector(collector *har.Collector) func() {
	harState.Lock()
	previous := harState.collector
	harState.collector = collector
	harState.Unlock()

	return func() {
		harState.Lock()
		harState.collector = previous
		harState.Unlock()
	}
}

func currentHARCollector() *har.Collector {
	harState.RLock()
	defer harState.RUnlock()
	return harState.collector
}

// Apply wires a commons HTTP client into the named http logger. Use
// ApplyWith if you also want HAR capture.
func Apply(client *commonshttp.Client) *commonshttp.Client {
	return ApplyWith(client, currentHARCollector())
}

// ApplyWith adds the standard HTTP request/response logger and, when
// non-nil, attaches a HAR collector that records every round trip into the
// caller's collector for later persistence.
func ApplyWith(client *commonshttp.Client, collector *har.Collector) *commonshttp.Client {
	if client == nil {
		return nil
	}
	if collector == nil {
		collector = currentHARCollector()
	}
	client = client.Use(func(rt nethttp.RoundTripper) nethttp.RoundTripper {
		return newHTTPLogRoundTripper(logger.GetLogger("http"), rt)
	})
	if collector != nil {
		client = client.HARCollector(collector)
	}
	return client
}

type httpLogOptions struct {
	summary      bool
	headers      bool
	requestBody  bool
	responseBody bool
}

func currentHTTPLogOptions(log logger.Logger) httpLogOptions {
	return httpLogOptionsFor(log.IsLevelEnabled)
}

func httpLogOptionsFor(enabled func(logger.LogLevel) bool) httpLogOptions {
	return httpLogOptions{
		summary:      enabled(logger.Debug),
		headers:      enabled(logger.Trace),
		requestBody:  enabled(logger.Trace1),
		responseBody: enabled(logger.Trace2),
	}
}

func newHTTPLogRoundTripper(log logger.Logger, rt nethttp.RoundTripper) nethttp.RoundTripper {
	opts := currentHTTPLogOptions(log)
	if !opts.summary {
		return rt
	}

	if opts.headers || opts.requestBody || opts.responseBody {
		pretty := &httpretty.Logger{
			SkipRequestInfo: true,
			RequestHeader:   opts.headers,
			ResponseHeader:  opts.headers,
			RequestBody:     opts.requestBody,
			ResponseBody:    opts.responseBody,
			Auth:            opts.headers,
			Colors:          true,
			Formatters:      []httpretty.Formatter{&httpretty.JSONFormatter{}},
			MaxRequestBody:  int64(properties.Int(4*1024, "http.log.request.body.length")),
			MaxResponseBody: int64(properties.Int(4*1024, "http.log.response.body.length")),
		}
		pretty.SkipHeader(logger.SensitiveHeaders)
		pretty.SetOutput(logger.GetOutput())
		rt = pretty.RoundTripper(rt)
	}

	return httpSummaryRoundTripper{log: log, next: rt}
}

type httpSummaryRoundTripper struct {
	log  logger.Logger
	next nethttp.RoundTripper
}

func (rt httpSummaryRoundTripper) RoundTrip(req *nethttp.Request) (*nethttp.Response, error) {
	start := time.Now()
	resp, err := rt.next.RoundTrip(req)
	rt.log.V(logger.Debug).Infof("%s\t%s\t%s\t%s",
		console.Greenf("%s", req.Method),
		req.URL.String(),
		formatHTTPStatus(resp, err),
		time.Since(start),
	)
	return resp, err
}

func formatHTTPStatus(resp *nethttp.Response, err error) string {
	if err != nil {
		return console.Redf("ERR")
	}
	if resp == nil {
		return console.Redf("NO RESPONSE")
	}
	status := resp.StatusCode
	switch {
	case status >= 500:
		return console.Redf("%d", status)
	case status >= 400:
		return console.Yellowf("%d", status)
	default:
		return console.Greenf("%d", status)
	}
}
