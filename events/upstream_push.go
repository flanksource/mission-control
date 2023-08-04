package events

import (
	"fmt"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/upstream"
	"github.com/flanksource/incident-commander/api"
	"gorm.io/gorm"
)

var upstreamPushEventHandler *pushToUpstreamEventHandler

func NewUpstreamPushConsumer(db *gorm.DB, config Config) EventConsumer {
	if config.UpstreamPush.Valid() {
		upstreamPushEventHandler = newPushToUpstreamEventHandler(config.UpstreamPush)
	}

	return EventConsumer{
		WatchEvents:      []string{EventPushQueueCreate},
		ProcessBatchFunc: handleUpstreamPushEvents,
		BatchSize:        50,
		Consumers:        5,
		DB:               db,
	}
}

func handleUpstreamPushEvents(ctx *api.Context, events []api.Event) []*api.Event {
	if upstreamPushEventHandler == nil {
		logger.Fatalf("Got push events but host is not configured")
	}

	var failedEvents []*api.Event
	var eventsToProcess []api.Event
	for _, e := range events {
		if e.Name != EventPushQueueCreate {
			e.Error = fmt.Errorf("Unrecognized event name: %s", e.Name).Error()
			failedEvents = append(failedEvents, &e)
		} else {
			eventsToProcess = append(eventsToProcess, e)
		}
	}

	failedEvents = append(failedEvents, upstreamPushEventHandler.Run(ctx, eventsToProcess)...)
	return failedEvents
}

type pushToUpstreamEventHandler struct {
	conf upstream.UpstreamConfig
}

func newPushToUpstreamEventHandler(conf upstream.UpstreamConfig) *pushToUpstreamEventHandler {
	return &pushToUpstreamEventHandler{
		conf: conf,
	}
}

func addErrorToFailedEvents(events []api.Event, err error) []*api.Event {
	var failedEvents []*api.Event
	for _, e := range events {
		e.Error = err.Error()
		failedEvents = append(failedEvents, &e)
	}
	return failedEvents
}

// Run pushes data from decentralized instances to central incident commander
func (t *pushToUpstreamEventHandler) Run(ctx *api.Context, events []api.Event) []*api.Event {
	gormDB := ctx.DB()
	upstreamMsgAggr := &upstream.PushData{
		AgentName: t.conf.AgentName,
	}

	var failedEvents []*api.Event
	for _, cl := range GroupChangelogsByTables(events) {
		upstreamMsg := &upstream.PushData{
			AgentName: t.conf.AgentName,
		}

		switch cl.tableName {
		case "topologies":
			if err := gormDB.Where("id IN ?", cl.itemIDs).Find(&upstreamMsg.Topologies).Error; err != nil {
				errMsg := fmt.Errorf("error fetching topologies: %w", err)
				failedEvents = append(failedEvents, addErrorToFailedEvents(cl.events, errMsg)...)
			}
			upstreamMsgAggr.Topologies = append(upstreamMsgAggr.Topologies, upstreamMsg.Topologies...)

		case "components":
			if err := gormDB.Where("id IN ?", cl.itemIDs).Find(&upstreamMsg.Components).Error; err != nil {
				errMsg := fmt.Errorf("error fetching components: %w", err)
				failedEvents = append(failedEvents, addErrorToFailedEvents(cl.events, errMsg)...)
			}
			upstreamMsgAggr.Components = append(upstreamMsgAggr.Components, upstreamMsg.Components...)

		case "canaries":
			if err := gormDB.Where("id IN ?", cl.itemIDs).Find(&upstreamMsg.Canaries).Error; err != nil {
				errMsg := fmt.Errorf("error fetching canaries: %w", err)
				failedEvents = append(failedEvents, addErrorToFailedEvents(cl.events, errMsg)...)
			}
			upstreamMsgAggr.Canaries = append(upstreamMsgAggr.Canaries, upstreamMsg.Canaries...)

		case "checks":
			if err := gormDB.Where("id IN ?", cl.itemIDs).Find(&upstreamMsg.Checks).Error; err != nil {
				errMsg := fmt.Errorf("error fetching checks: %w", err)
				failedEvents = append(failedEvents, addErrorToFailedEvents(cl.events, errMsg)...)
			}
			upstreamMsgAggr.Checks = append(upstreamMsgAggr.Checks, upstreamMsg.Checks...)

		case "config_scrapers":
			if err := gormDB.Where("id IN ?", cl.itemIDs).Find(&upstreamMsg.ConfigAnalysis).Error; err != nil {
				errMsg := fmt.Errorf("error fetching config_scrapers: %w", err)
				failedEvents = append(failedEvents, addErrorToFailedEvents(cl.events, errMsg)...)
			}
			upstreamMsgAggr.ConfigScrapers = append(upstreamMsgAggr.ConfigScrapers, upstreamMsg.ConfigScrapers...)

		case "config_analysis":
			if err := gormDB.Where("id IN ?", cl.itemIDs).Find(&upstreamMsg.ConfigAnalysis).Error; err != nil {
				errMsg := fmt.Errorf("error fetching config_analysis: %w", err)
				failedEvents = append(failedEvents, addErrorToFailedEvents(cl.events, errMsg)...)
			}
			upstreamMsgAggr.ConfigAnalysis = append(upstreamMsgAggr.ConfigAnalysis, upstreamMsg.ConfigAnalysis...)

		case "config_changes":
			if err := gormDB.Where("id IN ?", cl.itemIDs).Find(&upstreamMsg.ConfigChanges).Error; err != nil {
				errMsg := fmt.Errorf("error fetching config_changes: %w", err)
				failedEvents = append(failedEvents, addErrorToFailedEvents(cl.events, errMsg)...)
			}
			upstreamMsgAggr.ConfigChanges = append(upstreamMsgAggr.ConfigChanges, upstreamMsg.ConfigChanges...)

		case "config_items":
			if err := gormDB.Where("id IN ?", cl.itemIDs).Find(&upstreamMsg.ConfigItems).Error; err != nil {
				errMsg := fmt.Errorf("error fetching config_items: %w", err)
				failedEvents = append(failedEvents, addErrorToFailedEvents(cl.events, errMsg)...)
			}
			upstreamMsgAggr.ConfigItems = append(upstreamMsgAggr.ConfigItems, upstreamMsg.ConfigItems...)

		case "check_statuses":
			if err := gormDB.Where(`(check_id, "time") IN ?`, cl.itemIDs).Find(&upstreamMsg.CheckStatuses).Error; err != nil {
				errMsg := fmt.Errorf("error fetching check_statuses: %w", err)
				failedEvents = append(failedEvents, addErrorToFailedEvents(cl.events, errMsg)...)
			}
			upstreamMsgAggr.CheckStatuses = append(upstreamMsgAggr.CheckStatuses, upstreamMsg.CheckStatuses...)

		case "config_component_relationships":
			if err := gormDB.Where("(component_id, config_id) IN ?", cl.itemIDs).Find(&upstreamMsg.ConfigComponentRelationships).Error; err != nil {
				errMsg := fmt.Errorf("error fetching config_component_relationships: %w", err)
				failedEvents = append(failedEvents, addErrorToFailedEvents(cl.events, errMsg)...)
			}
			upstreamMsgAggr.ConfigComponentRelationships = append(upstreamMsgAggr.ConfigComponentRelationships, upstreamMsg.ConfigComponentRelationships...)

		case "component_relationships":
			if err := gormDB.Where("(component_id, relationship_id, selector_id) IN ?", cl.itemIDs).Find(&upstreamMsg.ComponentRelationships).Error; err != nil {
				errMsg := fmt.Errorf("error fetching component_relationships: %w", err)
				failedEvents = append(failedEvents, addErrorToFailedEvents(cl.events, errMsg)...)
			}
			upstreamMsgAggr.ComponentRelationships = append(upstreamMsgAggr.ComponentRelationships, upstreamMsg.ComponentRelationships...)

		case "config_relationships":
			if err := gormDB.Where("(related_id, config_id, selector_id) IN ?", cl.itemIDs).Find(&upstreamMsg.ConfigRelationships).Error; err != nil {
				errMsg := fmt.Errorf("error fetching config_relationships: %w", err)
				failedEvents = append(failedEvents, addErrorToFailedEvents(cl.events, errMsg)...)
			}
			upstreamMsgAggr.ConfigRelationships = append(upstreamMsgAggr.ConfigRelationships, upstreamMsg.ConfigRelationships...)
		}
	}

	upstreamMsgAggr.ApplyLabels(t.conf.LabelsMap())
	if err := upstream.Push(ctx, t.conf, upstreamMsgAggr); err != nil {
		errMsg := fmt.Errorf("failed to push to upstream: %w", err)
		failedEvents = append(failedEvents, addErrorToFailedEvents(events, errMsg)...)
	}

	return failedEvents
}

type GroupedPushEvents struct {
	tableName string
	itemIDs   [][]string
	events    []api.Event
}

// GroupChangelogsByTables groups the given events by the table they belong to.
func GroupChangelogsByTables(events []api.Event) []GroupedPushEvents {
	type pushEvent struct {
		tableName string
		itemIDs   []string
		event     api.Event
	}
	var pushEvents []pushEvent
	for _, cl := range events {
		tableName := cl.Properties["table"]
		var itemIDs []string
		switch tableName {
		case "component_relationships":
			itemIDs = []string{cl.Properties["component_id"], cl.Properties["relationship_id"], cl.Properties["selector_id"]}
		case "config_component_relationships":
			itemIDs = []string{cl.Properties["component_id"], cl.Properties["config_id"]}
		case "config_relationships":
			itemIDs = []string{cl.Properties["related_id"], cl.Properties["config_id"], cl.Properties["selector_id"]}
		case "check_statuses":
			itemIDs = []string{cl.Properties["check_id"], cl.Properties["time"]}
		default:
			itemIDs = []string{cl.Properties["id"]}
		}
		pe := pushEvent{
			tableName: tableName,
			itemIDs:   itemIDs,
			event:     cl,
		}
		pushEvents = append(pushEvents, pe)
	}

	tblPushMap := make(map[string]*GroupedPushEvents)
	var group []GroupedPushEvents
	for _, p := range pushEvents {
		if k, exists := tblPushMap[p.tableName]; exists {
			k.itemIDs = append(k.itemIDs, p.itemIDs)
			k.events = append(k.events, p.event)
		} else {
			gp := &GroupedPushEvents{
				tableName: p.tableName,
				itemIDs:   [][]string{p.itemIDs},
				events:    []api.Event{p.event},
			}
			tblPushMap[p.tableName] = gp
		}
	}

	for _, v := range tblPushMap {
		group = append(group, *v)
	}
	return group
}
