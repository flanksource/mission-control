package rules

import (
	"context"
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
	ctx := context.Background()

	if err := db.Gorm.
		// .Order("priority ASC")
		Find(&Rules).Error; err != nil {
		return err
	}

	statuses := getAllStatii()
	response, err := duty.QueryTopology(ctx, db.Pool, duty.TopologyOptions{
		Flatten: true,
		Status:  statuses,
	})
	if err != nil {
		return err
	}
	logger.Debugf("Found %d components with statuses: %v", len(response.Components), statuses)

	autoCreatedOpenIncidents, err := getOpenIncidentsWithRules(ctx)
	if err != nil {
		return err
	}

	return createIncidents(autoCreatedOpenIncidents, response.Components)
}

// createIncidents creates incidents based on the components
// and incident rules.
func createIncidents(openIncidentsMap map[string]map[string]struct{}, components dutyModels.Components) error {
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

				incident := rule.Template.GenerateIncident()
				incident.IncidentRuleID = _rule.ID
				incident.Status = api.IncidentStatusOpen
				incident.Severity = "Low" // TODO: make this configurable
				if incident.Type == "" {
					incident.Type = api.IncidentTypeAvailability
				}
				incident.Title = component.Name + " is " + string(component.Status)

				if _, ok := openIncidentsMap[_rule.ID.String()][component.ID.String()]; ok {
					logger.Debugf("Incident %s already exists", incident.Title)
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

// getOpenIncidentsWithRules generates a map linking incident rules, which led to the creation of open incidents, to their respective involved component ids.
func getOpenIncidentsWithRules(ctx context.Context) (map[string]map[string]struct{}, error) {
	query := `
	SELECT
		incidents.incident_rule_id,
		evidences.component_id
	FROM
		incidents
		LEFT JOIN hypotheses ON hypotheses.incident_id = incidents.id
		LEFT JOIN evidences ON evidences.hypothesis_id = hypotheses.id
	WHERE
		incidents.incident_rule_id IS NOT NULL
		AND evidences.component_id IS NOT NULL
		AND incidents.status = ?`
	rows, err := db.Gorm.WithContext(ctx).Raw(query, api.IncidentStatusOpen).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	autoCreatedOpenIncidents := make(map[string]map[string]struct{})
	for rows.Next() {
		var (
			ruleID      string
			componentID string
		)
		if err := rows.Scan(&ruleID, &componentID); err != nil {
			return nil, err
		}

		if _, ok := autoCreatedOpenIncidents[ruleID]; !ok {
			autoCreatedOpenIncidents[ruleID] = make(map[string]struct{})
		}
		autoCreatedOpenIncidents[ruleID][componentID] = struct{}{}
	}
	logger.Debugf("Found %d open incidents created by incident rules.", len(autoCreatedOpenIncidents))
	return autoCreatedOpenIncidents, nil
}
