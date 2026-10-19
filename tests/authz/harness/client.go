package harness

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

var httpClient = &http.Client{Timeout: 30 * time.Second}

// Request is a request to Mission Control's public API.
type Request struct {
	Method string
	Path   string
	Body   any
}

func (r Request) String() string {
	return r.Method + " " + r.Path
}

// Response is what Mission Control answered.
type Response struct {
	Request Request
	Status  int
	Body    []byte
	Err     error
}

func (r Response) String() string {
	if r.Err != nil {
		return fmt.Sprintf("%s: %v", r.Request, r.Err)
	}
	body := string(r.Body)
	if len(body) > 600 {
		body = body[:600] + "..."
	}
	return fmt.Sprintf("%s: %d %s", r.Request, r.Status, body)
}

// Decode unmarshals the body into v.
func (r Response) Decode(v any) error {
	if err := json.Unmarshal(r.Body, v); err != nil {
		return fmt.Errorf("decoding %s: %w", r, err)
	}
	return nil
}

// Caller makes requests as one person, with their password or one of their access tokens.
type Caller struct {
	name     string
	username string
	password string
	token    string
	server   *Server
}

// Email is the email of the person the caller signs in as.
func (c *Caller) Email() string {
	return c.username
}

func (c *Caller) String() string {
	return c.name
}

// WithToken returns a caller that signs in as the same person with an access token.
func (c *Caller) WithToken(name, token string) *Caller {
	return &Caller{name: c.name + " (token " + name + ")", username: c.username, token: token, server: c.server}
}

// Anonymous returns a caller that sends no credentials.
func (c *Caller) Anonymous() *Caller {
	return &Caller{name: "anonymous", server: c.server}
}

// Do sends the request.
func (c *Caller) Do(r Request) Response {
	resp := Response{Request: r}

	var body io.Reader
	if r.Body != nil {
		raw, err := json.Marshal(r.Body)
		if err != nil {
			resp.Err = err
			return resp
		}
		body = bytes.NewReader(raw)
	}

	req, err := http.NewRequest(r.Method, c.server.URL+r.Path, body)
	if err != nil {
		resp.Err = err
		return resp
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	// PostgREST answers a write with the rows it wrote
	req.Header.Set("Prefer", "return=representation")
	switch {
	case c.token != "":
		req.Header.Set("Authorization", "Bearer "+c.token)
	case c.username != "":
		req.SetBasicAuth(c.username, c.password)
	}

	res, err := httpClient.Do(req)
	if err != nil {
		resp.Err = err
		return resp
	}
	defer res.Body.Close()
	resp.Status = res.StatusCode
	resp.Body, resp.Err = io.ReadAll(res.Body)
	return resp
}

func randomSuffix() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
