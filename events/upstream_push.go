package events

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	"gorm.io/gorm"
)

type pushToUpstreamEventHandler struct {
	conf       api.UpstreamConfig
	httpClient *http.Client
}

func newPushToUpstreamEventHandler(conf api.UpstreamConfig) *pushToUpstreamEventHandler {
	return &pushToUpstreamEventHandler{
		conf:       conf,
		httpClient: &http.Client{},
	}
}

// Run pushes data from decentralized instances to central incident commander
func (t *pushToUpstreamEventHandler) Run(ctx context.Context, tx *gorm.DB, event api.Event) error {
	pushQueueID := event.Properties["id"]
	var changelogs []models.PushQueue
	if err := tx.Where("id = ?", pushQueueID).Find(&changelogs).Error; err != nil {
		return fmt.Errorf("error querying push_queue: %w", err)
	}

	logger.Debugf("found %d items in push_queue", len(changelogs))
	if len(changelogs) == 0 {
		return nil
	}

	upstreamMsg := &api.PushData{
		CheckedAt: time.Now(),
	}

	for tablename, itemIDs := range groupChangelogsByTables(changelogs) {
		switch tablename {
		case "components":
			if err := tx.Where("id IN ?", itemIDs).Find(&upstreamMsg.Components).Error; err != nil {
				return fmt.Errorf("error fetching components: %w", err)
			}

		case "canaries":
			if err := tx.Where("id IN ?", itemIDs).Find(&upstreamMsg.Canaries).Error; err != nil {
				return fmt.Errorf("error fetching canaries: %w", err)
			}

		case "checks":
			if err := tx.Where("id IN ?", itemIDs).Find(&upstreamMsg.Checks).Error; err != nil {
				return fmt.Errorf("error fetching checks: %w", err)
			}

		case "config_analysis":
			if err := tx.Where("id IN ?", itemIDs).Find(&upstreamMsg.ConfigAnalysis).Error; err != nil {
				return fmt.Errorf("error fetching config_analysis: %w", err)
			}

		case "config_changes":
			if err := tx.Where("id IN ?", itemIDs).Find(&upstreamMsg.ConfigChanges).Error; err != nil {
				return fmt.Errorf("error fetching config_changes: %w", err)
			}

		case "config_items":
			if err := tx.Where("id IN ?", itemIDs).Find(&upstreamMsg.ConfigItems).Error; err != nil {
				return fmt.Errorf("error fetching config_items: %w", err)
			}

		case "check_statuses":
			compositeKeys := splitKeys(itemIDs)
			if err := tx.Where(`(check_id, "time") IN ?`, compositeKeys).Find(&upstreamMsg.CheckStatuses).Error; err != nil {
				return fmt.Errorf("error fetching check_statuses: %w", err)
			}

		case "config_component_relationships":
			compositeKeys := splitKeys(itemIDs)
			if err := tx.Where("(component_id, config_id) IN ?", compositeKeys).Find(&upstreamMsg.ConfigItems).Error; err != nil {
				return fmt.Errorf("error fetching config_component_relationships: %w", err)
			}

		case "component_relationships":
			compositeKeys := splitKeys(itemIDs)
			if err := tx.Where("(component_id, relationship_id, selector_id) IN ?", compositeKeys).Find(&upstreamMsg.ComponentRelationships).Error; err != nil {
				return fmt.Errorf("error fetching component_relationships: %w", err)
			}

		case "config_relationships":
			keys := splitKeys(itemIDs)
			if err := tx.Where("(related_id, config_id, selector_id) IN ?", keys).Find(&upstreamMsg.ConfigRelationships).Error; err != nil {
				return fmt.Errorf("error fetching config_relationships: %w", err)
			}
		}
	}

	if err := t.push(ctx, upstreamMsg); err != nil {
		return fmt.Errorf("failed to push to upstream: %w", err)
	}

	return nil
}

func (t *pushToUpstreamEventHandler) push(ctx context.Context, msg *api.PushData) error {
	payloadBuf := new(bytes.Buffer)
	if err := json.NewEncoder(payloadBuf).Encode(msg); err != nil {
		return fmt.Errorf("error encoding msg: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, t.conf.URL, payloadBuf)
	if err != nil {
		return fmt.Errorf("http.NewRequest: %w", err)
	}

	req.SetBasicAuth(t.conf.Username, t.conf.Password)
	resp, err := t.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("error making request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("upstream server returned error status: %s", resp.Status)
	}

	return nil
}

func groupChangelogsByTables(changelogs []models.PushQueue) map[string][]string {
	var output = make(map[string][]string)
	for _, cl := range changelogs {
		output[cl.Table] = append(output[cl.Table], cl.ItemID)
	}

	return output
}

// splitKeys splits each item in the string slice by ':'
func splitKeys(keys []string) [][]string {
	output := make([][]string, 0, len(keys))
	for _, k := range keys {
		keysSplit := strings.Split(k, ":")
		output = append(output, keysSplit)
	}

	return output
}
