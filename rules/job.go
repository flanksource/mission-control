package rules

import (
	"context"
	"io"
	"strings"
	"time"

	"github.com/antihax/optional"
	sdk "github.com/flanksource/canary-checker/sdk"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/db/models"
	"github.com/flanksource/incident-commander/topology"
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
	logger.Debugf("Checking rules every %v", Period)

	components, resp, err := topology.Service().TopologyQuery(context.Background(), &sdk.TopologyApiTopologyQueryOpts{
		Flatten: optional.NewString("true"),
		Status:  optional.NewString(strings.Join(getAllStatii(), ",")),
	})
	if err != nil {
		if resp != nil {
			s, _ := io.ReadAll(resp.Body)
			logger.Errorf("Failed to query topology: %v", string(s))
		}
		return err
	}

	logger.Infof("Found %d components", len(components))
	return CreateIncidents(components)
}

// CreateIncidents creates incidents based on the components
// and incident rules
func CreateIncidents(components []sdk.Component) error {
	if err := db.Gorm.
		// .Order("priority ASC")
		Find(&Rules).Error; err != nil {
		return err
	}

outer:
	for _, component := range components {
		for _, _rule := range Rules {
			rule, err := _rule.GetSpec()
			if err != nil {
				logger.Errorf("error fetching rule spec: %s, %v", _rule.Name, err)
				continue
			}

			if matches(rule, component) {
				logger.Infof("Rule %s matched component %s", rule, component)

				incident := rule.Template
				incident.IncidentRuleID = _rule.ID
				incident.Status = api.IncidentStatusOpen
				if incident.Type == "" {
					incident.Type = api.IncidentTypeAvailability
				}
				incident.Title = component.Name + " is " + string(component.Status)
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
					ComponentID:      component.GetUUID(),
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

func matches(rule *api.IncidentRuleSpec, component sdk.Component) bool {
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

func matchesSelector(selector api.ComponentSelector, component sdk.Component) bool {
	if selector.Name != "" && selector.Name != component.Name {
		return false
	}

	if selector.Namespace != "" && selector.Namespace != component.Namespace {
		return false
	}

	if !selector.Types.Contains(component.Type_) {
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

type IncidentsByRules struct {
	api.Incident
	api.IncidentRuleSpec
}

func StartJob() error {
	// for {
	// 	time.Sleep(Period)

	// 	db.Gorm.Model(&models.ConfigAnalysis)
	// }
	return nil
}
