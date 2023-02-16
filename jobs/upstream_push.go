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
	"github.com/flanksource/incident-commander/db/models"
	"github.com/google/uuid"
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

	if err := t.run(ctx); err != nil {
		logger.Errorf("t.run(); %v", err)
	}
}

// pushToUpstream pushes data from decentralized instances to central incident commander
func (t *pushToUpstreamJob) run(ctx context.Context) error {
	var changelogs []models.Changelog

	if err := db.Gorm.WithContext(ctx).Table("changelog").Select("DISTINCT item_id, id, tstamp, tablename").Where("tstamp >= ?", t.lastCheckedAt.UTC()).Find(&changelogs).Error; err != nil {
		return fmt.Errorf("error fetching changelog; %w", err)
	}

	logger.Debugf("Found %d changelogs since %s", len(changelogs), t.lastCheckedAt)

	if len(changelogs) == 0 {
		return nil
	}

	upstreamMsg := &api.ConfigChanges{
		PreviousCheck: t.lastCheckedAt,
		CheckedAt:     time.Now(),
	}

	t.lastCheckedAt = time.Now()

	for tablename, cl := range groupChangelogsByTables(changelogs) {
		switch tablename {
		case "components":
			if err := db.Gorm.WithContext(ctx).Table("components").Select("*").Where("id IN ?", cl).Find(&upstreamMsg.Components).Error; err != nil {
				return fmt.Errorf("error fetching components; %w", err)
			}

		case "canaries":
			if err := db.Gorm.WithContext(ctx).Table("canaries").Select("*").Where("id IN ?", cl).Find(&upstreamMsg.Canaries).Error; err != nil {
				return fmt.Errorf("error fetching canaries; %w", err)
			}

		case "checks":
			if err := db.Gorm.WithContext(ctx).Table("checks").Select("*").Where("id IN ?", cl).Find(&upstreamMsg.Checks).Error; err != nil {
				return fmt.Errorf("error fetching checks; %w", err)
			}

		case "config_analysis":
			if err := db.Gorm.WithContext(ctx).Table("config_analysis").Select("*").Where("id IN ?", cl).Find(&upstreamMsg.ConfigAnalysis).Error; err != nil {
				return fmt.Errorf("error fetching config_analysis; %w", err)
			}

		case "config_changes":
			if err := db.Gorm.WithContext(ctx).Table("config_changes").Select("*").Where("id IN ?", cl).Find(&upstreamMsg.ConfigChanges).Error; err != nil {
				return fmt.Errorf("error fetching config_changes; %w", err)
			}

		case "config_items":
			if err := db.Gorm.WithContext(ctx).Table("config_items").Select("*").Where("id IN ?", cl).Find(&upstreamMsg.ConfigItems).Error; err != nil {
				return fmt.Errorf("error fetching config_items; %w", err)
			}

		case "check_statuses":
			if err := db.Gorm.WithContext(ctx).Table("check_statuses").Select("*").Where("check_id IN ?", cl).Find(&upstreamMsg.CheckStatuses).Error; err != nil {
				return fmt.Errorf("error fetching check_statuses; %w", err)
			}

		case "config_component_relationships": // TODO: This has composite primary keys
			if err := db.Gorm.WithContext(ctx).Table("config_component_relationships").Select("*").Where("component_id IN ?", cl).Find(&upstreamMsg.ConfigItems).Error; err != nil {
				return fmt.Errorf("error fetching config_component_relationships; %w", err)
			}

		case "component_relationships": // TODO: This has composite primary keys
			if err := db.Gorm.WithContext(ctx).Table("component_relationships").Select("*").Where("component_id IN ?", cl).Find(&upstreamMsg.ComponentRelationships).Error; err != nil {
				return fmt.Errorf("error fetching component_relationships; %w", err)
			}

		case "config_relationships": // TODO: This has composite primary keys
			if err := db.Gorm.WithContext(ctx).Table("config_relationships").Select("*").Where("config_id IN ?", cl).Find(&upstreamMsg.ConfigRelationships).Error; err != nil {
				return fmt.Errorf("error fetching config_relationships; %w", err)
			}
		}
	}

	if err := t.push(ctx, upstreamMsg); err != nil {
		return fmt.Errorf("failed to push to upstream; %w", err)
	}

	return nil
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

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("upstream server returned error status; %s", resp.Status)
	}

	return nil
}

func groupChangelogsByTables(changelogs []models.Changelog) map[string][]uuid.UUID {
	var output = make(map[string][]uuid.UUID)
	for _, cl := range changelogs {
		output[cl.TableName] = append(output[cl.TableName], cl.ItemID)
	}

	return output
}
