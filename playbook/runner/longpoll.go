package runner

import (
	"encoding/json"
	"time"

	"github.com/flanksource/duty/postq/pg"
)

var DefaultLongpollTimeout = time.Minute

// Global instance
var ActionNotifyRouter = pg.NewNotifyRouter().WithRouteExtractor(playbookActionNotifyRouteExtractor)

type playbookActionNotifyPayload struct {
	ID      string `json:"id"`
	AgentID string `json:"agent_id"`
}

func playbookActionNotifyRouteExtractor(payload string) (string, string, error) {
	var p playbookActionNotifyPayload
	if err := json.Unmarshal([]byte(payload), &p); err != nil {
		return "", "", err
	}

	route := p.AgentID
	extractedPayload := p.ID

	return route, extractedPayload, nil
}
