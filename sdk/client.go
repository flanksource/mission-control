package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/flanksource/commons/http"
	"github.com/flanksource/duty/models"
)

// ErrHTMLResponse is returned when the server responded with HTML on a JSON
// endpoint — typically because the configured server URL points at the
// user-facing frontend rather than the /api backend.
var ErrHTMLResponse = errors.New("server returned HTML instead of JSON (is the backend at /api?)")

type Client struct {
	*http.Client
}

func New(serverURL, token string) *Client {
	return &Client{
		Client: http.NewClient().
			BaseURL(serverURL).
			Header("Authorization", "Bearer "+token).
			Header("Accept", "application/json").
			Header("Content-Type", "application/json").
			UserAgent("mission-control-cli"),
	}
}

// decodeJSON parses a response body as JSON, returning ErrHTMLResponse if the
// body looks like HTML (frontend served instead of backend JSON).
func decodeJSON(r *http.Response, out any) error {
	body, err := r.AsString()
	if err != nil {
		return err
	}
	if looksLikeHTML(r.Header.Get("Content-Type"), body) {
		return ErrHTMLResponse
	}
	if err := json.Unmarshal([]byte(body), out); err != nil {
		return fmt.Errorf("failed to decode JSON response: %w", err)
	}
	return nil
}

func looksLikeHTML(contentType, body string) bool {
	if strings.Contains(strings.ToLower(contentType), "text/html") {
		return true
	}
	return strings.HasPrefix(strings.TrimLeft(body, " \t\r\n"), "<")
}

func (c *Client) GetConnection(name, namespace string) (*models.Connection, error) {
	var connections []models.Connection
	r, err := c.R(context.Background()).
		QueryParam("name", "eq."+name).
		QueryParam("namespace", "eq."+namespace).
		QueryParam("deleted_at", "is.null").
		QueryParam("limit", "1").
		Get("/db/connections")
	if err != nil {
		return nil, err
	}
	if !r.IsOK() {
		return nil, fmt.Errorf("server returned %d", r.StatusCode)
	}
	if err := decodeJSON(r, &connections); err != nil {
		return nil, err
	}
	if len(connections) == 0 {
		return nil, fmt.Errorf("connection %s/%s not found", namespace, name)
	}
	return &connections[0], nil
}

func (c *Client) SaveConnection(conn *models.Connection) error {
	r, err := c.R(context.Background()).
		Header("Prefer", "resolution=merge-duplicates,return=representation").
		Post("/db/connections", conn)
	if err != nil {
		return err
	}
	if !r.IsOK() {
		body, _ := r.AsString()
		return fmt.Errorf("server returned %d: %s", r.StatusCode, body)
	}
	return nil
}

type TestResult struct {
	Message string `json:"message"`
	Payload any    `json:"payload"`
}

func (c *Client) TestConnection(id string) (*TestResult, error) {
	var result TestResult
	r, err := c.R(context.Background()).
		Post("/connection/test/"+url.PathEscape(id), nil)
	if err != nil {
		return nil, err
	}
	if !r.IsOK() {
		body, _ := r.AsString()
		if looksLikeHTML(r.Header.Get("Content-Type"), body) {
			return nil, ErrHTMLResponse
		}
		return nil, fmt.Errorf("test failed (%d): %s", r.StatusCode, body)
	}
	if err := decodeJSON(r, &result); err != nil {
		return &result, err
	}
	return &result, nil
}
