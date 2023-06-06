package rules

import (
	"context"
	"fmt"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty"
	dutyModels "github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/db/models"
)

var Period = time.Second * 300

var Rules []models.IncidentRule

func getAllStatii() []string {
	var statii []string
	for _, rule := range Rules {
		spec, _ := rule.GetSpec()
		if spec != nil {
			statii = append(statii, spec.Filter.Status...)
		}
	}
	return statii
}

func Run() error {
	if err := db.Gorm.
		// .Order("priority ASC")
		Find(&Rules).Error; err != nil {
		return err
	}

	statuses := getAllStatii()
	response, err := duty.QueryTopology(context.Background(), db.Pool, duty.TopologyOptions{
		Flatten: true,
		Status:  statuses,
	})
	if err != nil {
		return err
	}
	logger.Infof("Found %d components with statuses: %v", len(response.Components), statuses)

	var autoCreatedOpenIncidents []dutyModels.Incident
	if err := db.Gorm.Find(&autoCreatedOpenIncidents, "incident_rule_id IS NOT NULL AND status = ?", api.IncidentStatusOpen).Error; err != nil {
		return fmt.Errorf("error getting open incidents: %w", err)
	}
	logger.Infof("Found %d open incidents created by incident rules.", len(autoCreatedOpenIncidents))

	return createIncidents(autoCreatedOpenIncidents, response.Components)
}

// createIncidents creates incidents based on the components
// and incident rules.
func createIncidents(openIncidents []dutyModels.Incident, components dutyModels.Components) error {
	openIncidentsMap := make(map[string]map[string]struct{})
	for _, incident := range openIncidents {
		if _, ok := openIncidentsMap[incident.IncidentRuleID.String()]; !ok {
			openIncidentsMap[incident.IncidentRuleID.String()] = make(map[string]struct{})
		}

		openIncidentsMap[incident.IncidentRuleID.String()][incident.Title] = struct{}{}
	}

outer:
	for _, component := range components {
		for _, _rule := range Rules {
			rule, err := _rule.GetSpec()
			if err != nil {
				logger.Errorf("error fetching rule spec: %s, %v", _rule.Name, err)
				continue
			}

			if matches(rule, *component) {
				logger.Infof("Rule %s matched component %s", rule, component)

				incident := rule.Template
				incident.IncidentRuleID = _rule.ID
				incident.Status = api.IncidentStatusOpen
				incident.Severity = "Low" // TODO: make this configurable
				if incident.Type == "" {
					incident.Type = api.IncidentTypeAvailability
				}
				incident.Title = component.Name + " is " + string(component.Status)

				if _, ok := openIncidentsMap[_rule.ID.String()][incident.Title]; ok {
					logger.Infof("Incident %s already exists", incident.Title)
					continue // this rule already created this incident
				}

				if err := db.Gorm.Create(&incident).Error; err != nil {
					return err
				}

				hypothesis := api.Hypothesis{
					IncidentID: *incident.ID,
					Title:      component.Name + " is " + string(component.Status),
					Type:       "factor",
				}

				if err := db.Gorm.Create(&hypothesis).Error; err != nil {
					return err
				}

				evidence := api.Evidence{
					HypothesisID:     hypothesis.ID,
					ComponentID:      &component.ID,
					DefinitionOfDone: true,
					Type:             "topology",
					Description:      component.Name + " is " + string(component.Status),
				}

				if err := db.Gorm.Create(&evidence).Error; err != nil {
					return err
				}

				// create incident
				if rule.BreakOnMatch {
					continue outer
				}
			}
		}
	}

	return nil
}

func matches(rule *api.IncidentRuleSpec, component dutyModels.Component) bool {
	if len(rule.Filter.Status) > 0 && !contains(rule.Filter.Status, string(component.Status)) {
		return false
	}

	for _, selector := range rule.Components {
		if matchesSelector(selector, component) {
			return true
		}
	}

	return false
}

func matchesSelector(selector api.ComponentSelector, component dutyModels.Component) bool {
	if selector.Name != "" && selector.Name != component.Name {
		return false
	}

	if selector.Namespace != "" && selector.Namespace != component.Namespace {
		return false
	}

	if !selector.Types.Contains(component.Type) {
		return false
	}

	return true
}

func contains(list []string, item string) bool {
	for _, i := range list {
		if i == item {
			return true
		}
	}
	return false
}
