package clienthttp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/flanksource/commons/logger"
)

type Middleware func(http.RoundTripper) http.RoundTripper

type Client struct {
	httpClient *http.Client
	baseURL    string
	headers    http.Header
	userAgent  string
}

func NewClient() *Client {
	var transport http.RoundTripper = http.DefaultTransport
	transport = harRoundTripper{next: transport}
	transport = loggingRoundTripper{next: transport}
	return &Client{
		httpClient: &http.Client{Transport: transport, Timeout: 2 * time.Minute},
		headers:    make(http.Header),
		userAgent:  "mission-control-cli",
	}
}

func (c *Client) BaseURL(value string) *Client {
	c.baseURL = strings.TrimRight(value, "/")
	return c
}

func (c *Client) Header(key, value string) *Client {
	c.headers.Set(key, value)
	return c
}

func (c *Client) UserAgent(value string) *Client {
	c.userAgent = value
	return c
}

func (c *Client) Timeout(value time.Duration) *Client {
	c.httpClient.Timeout = value
	return c
}

func (c *Client) Use(middleware Middleware) *Client {
	if middleware != nil {
		c.httpClient.Transport = middleware(c.transport())
	}
	return c
}

func (c *Client) transport() http.RoundTripper {
	if c.httpClient.Transport == nil {
		return http.DefaultTransport
	}
	return c.httpClient.Transport
}

func (c *Client) R(ctx context.Context) *Request {
	return &Request{
		ctx:         ctx,
		client:      c,
		headers:     make(http.Header),
		queryParams: make(url.Values),
	}
}

type Request struct {
	ctx         context.Context
	client      *Client
	headers     http.Header
	queryParams url.Values
}

func (r *Request) Header(key, value string) *Request {
	r.headers.Set(key, value)
	return r
}

func (r *Request) QueryParam(key, value string) *Request {
	r.queryParams.Set(key, value)
	return r
}

func (r *Request) Get(path string) (*Response, error) {
	return r.do(http.MethodGet, path, nil)
}

func (r *Request) Post(path string, body any) (*Response, error) {
	return r.do(http.MethodPost, path, body)
}

func (r *Request) Put(path string, body any) (*Response, error) {
	return r.do(http.MethodPut, path, body)
}

func (r *Request) Patch(path string, body any) (*Response, error) {
	return r.do(http.MethodPatch, path, body)
}

func (r *Request) Delete(path string) (*Response, error) {
	return r.do(http.MethodDelete, path, nil)
}

func (r *Request) do(method, path string, body any) (*Response, error) {
	requestURL, err := r.resolveURL(path)
	if err != nil {
		return nil, err
	}

	reader, err := requestBody(body)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(r.ctx, method, requestURL, reader)
	if err != nil {
		return nil, err
	}
	for key, values := range r.client.headers {
		request.Header[key] = append([]string(nil), values...)
	}
	for key, values := range r.headers {
		request.Header[key] = append([]string(nil), values...)
	}
	if r.client.userAgent != "" {
		request.Header.Set("User-Agent", r.client.userAgent)
	}

	response, err := r.client.httpClient.Do(request)
	if err != nil {
		if urlError, ok := err.(*url.Error); ok {
			return nil, urlError.Err
		}
		return nil, err
	}
	return &Response{Response: response}, nil
}

func (r *Request) resolveURL(path string) (string, error) {
	parsed, err := url.Parse(path)
	if err != nil {
		return "", err
	}
	if !parsed.IsAbs() {
		if r.client.baseURL == "" {
			return "", fmt.Errorf("relative request path %q requires a base URL", path)
		}
		parsed, err = url.Parse(r.client.baseURL + "/" + strings.TrimLeft(path, "/"))
		if err != nil {
			return "", err
		}
	}
	query := parsed.Query()
	for key, values := range r.queryParams {
		query[key] = append([]string(nil), values...)
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func requestBody(body any) (io.Reader, error) {
	switch value := body.(type) {
	case nil:
		return nil, nil
	case io.Reader:
		return value, nil
	case []byte:
		return bytes.NewReader(value), nil
	case string:
		return strings.NewReader(value), nil
	default:
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		return bytes.NewReader(encoded), nil
	}
}

type Response struct {
	*http.Response
}

func (r *Response) IsOK(validCodes ...int) bool {
	if len(validCodes) == 0 {
		return r.StatusCode >= 200 && r.StatusCode < 300
	}
	for _, code := range validCodes {
		if r.StatusCode == code {
			return true
		}
	}
	return false
}

func (r *Response) AsString() (string, error) {
	defer r.Body.Close()
	value, err := io.ReadAll(r.Body)
	return string(value), err
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type loggingRoundTripper struct {
	next http.RoundTripper
}

func (t loggingRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	start := time.Now()
	response, err := t.next.RoundTrip(request)
	status := "ERR"
	if response != nil {
		status = fmt.Sprint(response.StatusCode)
	}
	logger.GetLogger("http").V(logger.Debug).Infof("%s\t%s\t%s\t%s", request.Method, redactURL(request.URL), status, time.Since(start))
	return response, err
}
