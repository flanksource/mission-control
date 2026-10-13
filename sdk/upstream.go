package sdk

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/flanksource/duty/upstream"
)

// PushUpstream posts a batch of agent-owned records to /upstream/push, the only
// server-side ingestion path for config items, changes and analyses (what the
// UI calls insights).
//
// agentName names the pushing agent. The endpoint only accepts an agent's own
// access token, and agentName must be that agent: any other token is rejected,
// and the error says so rather than reading as a generic 403.
func (c *Client) PushUpstream(ctx context.Context, agentName string, data *upstream.PushData) error {
	if agentName == "" {
		return fmt.Errorf("agent name is required to push upstream")
	}
	if data == nil || data.Count() == 0 {
		return nil
	}

	r, err := c.R(ctx).
		QueryParam(upstream.AgentNameQueryParam, agentName).
		Post(c.apiPath("/upstream/push"), data)
	if err != nil {
		return err
	}
	defer r.Body.Close()

	if r.IsOK() {
		return nil
	}

	body, _ := r.AsString()
	body = strings.TrimSpace(body)
	if looksLikeHTML(r.Header.Get("Content-Type"), body) {
		return fmt.Errorf("POST /upstream/push returned HTML with status %d: %w", r.StatusCode, ErrHTMLResponse)
	}
	if r.StatusCode == http.StatusForbidden || r.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("not permitted to push upstream as agent %q: /upstream/push only accepts the access token "+
			"of the agent it names (%s)", agentName, body)
	}
	return newServerError(r.StatusCode, []byte(body))
}
