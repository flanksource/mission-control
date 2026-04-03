package sdk

import (
	"context"
	"fmt"
	"net/url"

	"github.com/flanksource/commons/http"
	"github.com/flanksource/duty/models"
)

type Client struct {
	*http.Client
}

func New(serverURL, token string) *Client {
	return &Client{
		Client: http.NewClient().
			BaseURL(serverURL).
			Header("Authorization", "Bearer "+token).
			Header("Content-Type", "application/json").
			UserAgent("mission-control-cli"),
	}
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
	if err := r.Into(&connections); err != nil {
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
		return nil, fmt.Errorf("test failed (%d): %s", r.StatusCode, body)
	}
	if err := r.Into(&result); err != nil {
		return &result, err
	}
	return &result, nil
}
