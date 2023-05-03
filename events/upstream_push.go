package events

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

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
func (t *pushToUpstreamEventHandler) Run(ctx context.Context, tx *gorm.DB, events []api.Event) error {
	upstreamMsg := &api.PushData{
		ClusterName: t.conf.ClusterName,
	}

	for tablename, itemIDs := range GroupChangelogsByTables(events) {
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
			if err := tx.Where(`(check_id, "time") IN ?`, itemIDs).Find(&upstreamMsg.CheckStatuses).Error; err != nil {
				return fmt.Errorf("error fetching check_statuses: %w", err)
			}

		case "config_component_relationships":
			if err := tx.Where("(component_id, config_id) IN ?", itemIDs).Find(&upstreamMsg.ConfigComponentRelationships).Error; err != nil {
				return fmt.Errorf("error fetching config_component_relationships: %w", err)
			}

		case "component_relationships":
			if err := tx.Where("(component_id, relationship_id, selector_id) IN ?", itemIDs).Find(&upstreamMsg.ComponentRelationships).Error; err != nil {
				return fmt.Errorf("error fetching component_relationships: %w", err)
			}

		case "config_relationships":
			if err := tx.Where("(related_id, config_id, selector_id) IN ?", itemIDs).Find(&upstreamMsg.ConfigRelationships).Error; err != nil {
				return fmt.Errorf("error fetching config_relationships: %w", err)
			}
		}
	}

	upstreamMsg.ApplyLabels(t.conf.LabelsMap())
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

	endpoint, err := url.JoinPath(t.conf.Host, "upstream_push")
	if err != nil {
		return fmt.Errorf("error creating url endpoint for host %s: %w", t.conf.Host, err)
	}

	req, err := http.NewRequest(http.MethodPost, endpoint, payloadBuf)
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

// GroupChangelogsByTables groups the given events by the table they belong to.
//
// Return Value:
// - A map of table names to slices of (composite) primary key values.
func GroupChangelogsByTables(events []api.Event) map[string][][]string {
	var output = make(map[string][][]string)
	for _, cl := range events {
		tableName := cl.Properties["table"]
		switch tableName {
		case "component_relationships":
			output[tableName] = append(output[tableName], []string{cl.Properties["component_id"], cl.Properties["relationship_id"], cl.Properties["selector_id"]})
		case "config_component_relationships":
			output[tableName] = append(output[tableName], []string{cl.Properties["component_id"], cl.Properties["config_id"]})
		case "config_relationships":
			output[tableName] = append(output[tableName], []string{cl.Properties["related_id"], cl.Properties["config_id"], cl.Properties["selector_id"]})
		case "check_statuses":
			output[tableName] = append(output[tableName], []string{cl.Properties["check_id"], cl.Properties["time"]})
		default:
			output[tableName] = append(output[tableName], []string{cl.Properties["id"]})
		}
	}

	return output
}
