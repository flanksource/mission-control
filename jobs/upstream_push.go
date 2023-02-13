package jobs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
)

type pushToUpstreamJob struct {
	lastCheckedAt time.Time
	maxRunTimeout time.Duration
	conf          api.UpstreamConfig
	httpClient    *http.Client
}

func newPushToUpstreamJob(refTime time.Time, conf api.UpstreamConfig) *pushToUpstreamJob {
	return &pushToUpstreamJob{
		maxRunTimeout: time.Minute * 5,
		lastCheckedAt: refTime,
		conf:          conf,
		httpClient:    &http.Client{},
	}
}

// pushToUpstream pushes data from decentralized instances to central incident commander
func (t *pushToUpstreamJob) Run() {
	ctx, cancel := context.WithTimeout(context.Background(), t.maxRunTimeout)
	defer cancel()

	msg := &api.ConfigChanges{
		PreviousCheck: t.lastCheckedAt,
		CheckedAt:     time.Now(),
	}

	// 1. Should fetch all the rows that were changed since X
	if err := db.Gorm.WithContext(ctx).Table("components").Select("*").Where("created_at > ? OR updated_at > ?", t.lastCheckedAt, t.lastCheckedAt).Order("created_at DESC").Find(&msg.Components).Error; err != nil {
		logger.Errorf("Failed")
	}

	logger.Infof("Found %d msg.Components since %s", len(msg.Components), t.lastCheckedAt)
	t.lastCheckedAt = time.Now()

	if len(msg.Components) == 0 {
		return
	}

	// 2. Push to upstream
	if err := t.push(ctx, msg); err != nil {
		logger.Errorf("Failed to publish to upstream; %v", err)
	}
}

func (t *pushToUpstreamJob) push(ctx context.Context, msg *api.ConfigChanges) error {
	payloadBuf := new(bytes.Buffer)
	if err := json.NewEncoder(payloadBuf).Encode(msg); err != nil {
		return fmt.Errorf("error encoding msg; %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, t.conf.URL, payloadBuf)
	if err != nil {
		return fmt.Errorf("http.NewRequest; %w", err)
	}

	req.SetBasicAuth(t.conf.Username, t.conf.Password)
	resp, err := t.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("error making request; %w", err)
	}
	defer resp.Body.Close()

	return nil
}
