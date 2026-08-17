package clienthttp

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

type HARConfig struct {
	MaxBodySize         int64
	CaptureContentTypes []string
}

type HARCollector struct {
	config  HARConfig
	mu      sync.Mutex
	entries []HAREntry
}

func NewHARCollector(config HARConfig) *HARCollector {
	if config.MaxBodySize == 0 {
		config.MaxBodySize = 64 * 1024
	}
	if len(config.CaptureContentTypes) == 0 {
		config.CaptureContentTypes = []string{"application/json", "application/x-www-form-urlencoded"}
	}
	return &HARCollector{config: config}
}

func (c *HARCollector) Entries() []HAREntry {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]HAREntry(nil), c.entries...)
}

func (c *HARCollector) add(entry HAREntry) {
	c.mu.Lock()
	c.entries = append(c.entries, entry)
	c.mu.Unlock()
}

func (c *HARCollector) middleware(next http.RoundTripper) http.RoundTripper {
	return roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		started := time.Now()
		entry := HAREntry{
			StartedDateTime: started.UTC().Format(time.RFC3339Nano),
			Request:         captureRequest(request, c.config),
			Cache:           HARCache{},
		}
		response, err := next.RoundTrip(request)
		entry.Time = float64(time.Since(started).Microseconds()) / 1000
		entry.Timings.Wait = entry.Time
		if response != nil {
			entry.Response = captureResponse(response, c.config)
		}
		c.add(entry)
		return response, err
	})
}

type harRoundTripper struct {
	next http.RoundTripper
}

func (t harRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	collector := currentHARCollector()
	if collector == nil {
		return t.next.RoundTrip(request)
	}
	return collector.middleware(t.next).RoundTrip(request)
}

var harState struct {
	sync.RWMutex
	collector *HARCollector
}

func SetHARCollector(collector *HARCollector) func() {
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

func currentHARCollector() *HARCollector {
	harState.RLock()
	defer harState.RUnlock()
	return harState.collector
}

type HARFile struct {
	Log HARLog `json:"log"`
}

type HARLog struct {
	Version string     `json:"version"`
	Creator HARCreator `json:"creator"`
	Entries []HAREntry `json:"entries"`
}

type HARCreator struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type HAREntry struct {
	StartedDateTime string      `json:"startedDateTime"`
	Time            float64     `json:"time"`
	Request         HARRequest  `json:"request"`
	Response        HARResponse `json:"response"`
	Cache           HARCache    `json:"cache"`
	Timings         HARTimings  `json:"timings"`
}

type HARCache struct{}

type HARRequest struct {
	Method      string           `json:"method"`
	URL         string           `json:"url"`
	HTTPVersion string           `json:"httpVersion"`
	Cookies     []HARCookie      `json:"cookies"`
	Headers     []HARHeader      `json:"headers"`
	QueryString []HARQueryString `json:"queryString"`
	PostData    *HARPostData     `json:"postData,omitempty"`
	HeadersSize int              `json:"headersSize"`
	BodySize    int64            `json:"bodySize"`
}

type HARResponse struct {
	Status      int         `json:"status"`
	StatusText  string      `json:"statusText"`
	HTTPVersion string      `json:"httpVersion"`
	Cookies     []HARCookie `json:"cookies"`
	Headers     []HARHeader `json:"headers"`
	Content     HARContent  `json:"content"`
	RedirectURL string      `json:"redirectURL"`
	HeadersSize int         `json:"headersSize"`
	BodySize    int64       `json:"bodySize"`
}

type HARCookie struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type HARHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type HARQueryString struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type HARPostData struct {
	MimeType string `json:"mimeType"`
	Text     string `json:"text"`
}

type HARContent struct {
	Size      int64  `json:"size"`
	MimeType  string `json:"mimeType,omitempty"`
	Text      string `json:"text,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
}

type HARTimings struct {
	Send    float64 `json:"send"`
	Wait    float64 `json:"wait"`
	Receive float64 `json:"receive"`
}

func captureRequest(request *http.Request, config HARConfig) HARRequest {
	result := HARRequest{
		Method:      request.Method,
		URL:         redactURL(request.URL),
		HTTPVersion: request.Proto,
		Cookies:     []HARCookie{},
		Headers:     captureHeaders(request.Header),
		QueryString: captureQuery(request.URL.Query()),
		HeadersSize: -1,
		BodySize:    -1,
	}
	contentType := request.Header.Get("Content-Type")
	if request.Body != nil && captureContentType(contentType, config.CaptureContentTypes) {
		body, restored := captureBody(request.Body)
		request.Body = restored
		result.BodySize = int64(len(body))
		text, _ := safeBodyText(body, contentType, config.MaxBodySize)
		result.PostData = &HARPostData{MimeType: contentType, Text: text}
	}
	return result
}

func captureResponse(response *http.Response, config HARConfig) HARResponse {
	result := HARResponse{
		Status:      response.StatusCode,
		StatusText:  response.Status,
		HTTPVersion: response.Proto,
		Cookies:     []HARCookie{},
		Headers:     captureHeaders(response.Header),
		HeadersSize: -1,
		BodySize:    -1,
	}
	contentType := response.Header.Get("Content-Type")
	if response.Body != nil && (captureContentType(contentType, config.CaptureContentTypes) || response.StatusCode >= 400) {
		body, restored := captureBody(response.Body)
		response.Body = restored
		result.BodySize = int64(len(body))
		text, truncated := safeBodyText(body, contentType, config.MaxBodySize)
		result.Content = HARContent{Size: int64(len(body)), MimeType: contentType, Text: text, Truncated: truncated}
	}
	return result
}

func captureBody(body io.ReadCloser) ([]byte, io.ReadCloser) {
	all, _ := io.ReadAll(body)
	_ = body.Close()
	return all, io.NopCloser(bytes.NewReader(all))
}

func captureContentType(contentType string, allowed []string) bool {
	contentType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	for _, candidate := range allowed {
		if strings.HasPrefix(contentType, strings.ToLower(candidate)) {
			return true
		}
	}
	return false
}

func captureHeaders(headers http.Header) []HARHeader {
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]HARHeader, 0, len(headers))
	for _, key := range keys {
		for _, value := range headers.Values(key) {
			if sensitiveHeader(key) {
				value = "***"
			}
			result = append(result, HARHeader{Name: key, Value: value})
		}
	}
	return result
}

func captureQuery(query url.Values) []HARQueryString {
	keys := make([]string, 0, len(query))
	for key := range query {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var result []HARQueryString
	for _, key := range keys {
		for _, value := range query[key] {
			if sensitiveKey(key) {
				value = "***"
			}
			result = append(result, HARQueryString{Name: key, Value: value})
		}
	}
	return result
}

func redactURL(value *url.URL) string {
	if value == nil {
		return ""
	}
	clone := *value
	query := clone.Query()
	for key := range query {
		if sensitiveKey(key) {
			query.Set(key, "***")
		}
	}
	clone.RawQuery = query.Encode()
	return clone.String()
}

func safeBodyText(body []byte, contentType string, max int64) (string, bool) {
	redacted := redactBody(body, contentType)
	truncated := max > 0 && int64(len(body)) > max
	if max > 0 && int64(len(redacted)) > max {
		redacted = redacted[:max]
		truncated = true
	}
	return string(redacted), truncated
}

func redactBody(body []byte, contentType string) []byte {
	contentType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	switch {
	case strings.Contains(contentType, "json"):
		var value any
		if json.Unmarshal(body, &value) != nil {
			return []byte("[body omitted: invalid JSON could not be safely redacted]")
		}
		redactJSONValue(value)
		redacted, err := json.Marshal(value)
		if err != nil {
			return []byte("[body omitted: JSON could not be safely redacted]")
		}
		return redacted
	case contentType == "application/x-www-form-urlencoded":
		values, err := url.ParseQuery(string(body))
		if err != nil {
			return []byte("[body omitted: form data could not be safely redacted]")
		}
		for key := range values {
			if sensitiveKey(key) {
				values[key] = []string{"***"}
			}
		}
		return []byte(values.Encode())
	default:
		return body
	}
}

func redactJSONValue(value any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			if sensitiveKey(key) {
				typed[key] = "***"
			} else {
				redactJSONValue(nested)
			}
		}
	case []any:
		for _, nested := range typed {
			redactJSONValue(nested)
		}
	}
}

func sensitiveKey(key string) bool {
	key = strings.Trim(strings.TrimSpace(strings.ToLower(key)), "_-. ")
	for _, allowed := range []string{"grant_type", "token_type"} {
		if key == allowed {
			return false
		}
	}
	if key == "code" || key == "code_verifier" {
		return true
	}
	for _, marker := range []string{"user", "pass", "secret", "key", "token", "authorization", "sessionid", "sessid", "cookie"} {
		if strings.Contains(key, marker) {
			return true
		}
	}
	return false
}

func sensitiveHeader(key string) bool {
	key = strings.ToLower(key)
	for _, marker := range []string{"authorization", "bearer", "session", "sessid", "cookie", "token", "secret", "key", "password", "passwd", "pwd"} {
		if strings.Contains(key, marker) {
			return true
		}
	}
	return false
}
