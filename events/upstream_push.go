package events

import (
	"context"
	"fmt"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/upstream"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"gorm.io/gorm"
)

var upstreamPushEventHandler *pushToUpstreamEventHandler

var UpstreamPushConsumer = EventConsumer{
	WatchEvents: ConsumerResponder,
	HandleFunc:  handleUpstreamPushEvents,
	BatchSize:   50,
	Consumers:   5,
	DB:          db.Gorm,
}

func handleUpstreamPushEvents(ctx *api.Context, config Config, event api.Event) error {
	if upstreamPushEventHandler == nil {
		if config.UpstreamConf.Valid() {
			upstreamPushEventHandler = newPushToUpstreamEventHandler(config.UpstreamConf)
		} else {
			logger.Fatalf("Got push events but not configured")
		}
	}

	switch event.Name {
	case EventIncidentResponderAdded:
		return reconcileResponderEvent(ctx, event)
	case EventIncidentCommentAdded:
		return reconcileCommentEvent(ctx, event)
	default:
		return fmt.Errorf("Unrecognized event name: %s", event.Name)
	}
}

type pushToUpstreamEventHandler struct {
	conf upstream.UpstreamConfig
}

func newPushToUpstreamEventHandler(conf upstream.UpstreamConfig) *pushToUpstreamEventHandler {
	return &pushToUpstreamEventHandler{
		conf: conf,
	}
}

// Run pushes data from decentralized instances to central incident commander
func (t *pushToUpstreamEventHandler) Run(ctx context.Context, tx *gorm.DB, events []api.Event) error {
	upstreamMsg := &upstream.PushData{
		AgentName: t.conf.AgentName,
	}

	for tablename, itemIDs := range GroupChangelogsByTables(events) {
		switch tablename {
		case "topologies":
			if err := tx.Where("id IN ?", itemIDs).Find(&upstreamMsg.Topologies).Error; err != nil {
				return fmt.Errorf("error fetching topologies: %w", err)
			}

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

		case "config_scrapers":
			if err := tx.Where("id IN ?", itemIDs).Find(&upstreamMsg.ConfigAnalysis).Error; err != nil {
				return fmt.Errorf("error fetching config_scrapers: %w", err)
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
	if err := upstream.Push(ctx, t.conf, upstreamMsg); err != nil {
		return fmt.Errorf("failed to push to upstream: %w", err)
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
