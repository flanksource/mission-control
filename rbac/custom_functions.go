package rbac

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/casbin/govaluate"
	"github.com/flanksource/commons/collections"
	"github.com/flanksource/duty/models"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/google/uuid"
	"github.com/samber/lo"
)

func matchPerm(attr *models.ABACAttribute, _agents any, tagsEncoded string) (bool, error) {
	var rAgents []string
	switch v := _agents.(type) {
	case []any:
		rAgents = lo.Map(v, func(item any, _ int) string { return item.(string) })
	case string:
		if v != "" {
			rAgents = append(rAgents, v)
		}
	}

	rTags := collections.SelectorToMap(tagsEncoded)
	if attr.Config.ID != uuid.Nil {
		var agentsMatch = true
		if len(rAgents) > 0 {
			agentsMatch = lo.Contains(rAgents, attr.Config.AgentID.String())
		}

		tagsmatch := mapContains(rTags, attr.Config.Tags)
		return tagsmatch && agentsMatch, nil
	}

	return false, nil
}

type addableEnforcer interface {
	AddFunction(name string, function govaluate.ExpressionFunction)
}

func addCustomFunctions(enforcer addableEnforcer) {
	enforcer.AddFunction("matchPerm", func(args ...any) (any, error) {
		if len(args) != 3 {
			return false, fmt.Errorf("matchPerm needs 3 arguments. got %d", len(args))
		}

		obj := args[0]
		if _, ok := obj.(string); ok {
			// an object is required to satisfy the agents & tags requirement.
			// If a role is passed, we don't match this permission.
			return false, nil
		}

		attr, ok := obj.(*models.ABACAttribute)
		if !ok {
			return false, errors.New("[matchPerm] unknown input type: expected *models.ABACAttribute")
		}

		agents := args[1]
		tags := args[2]

		tagsEncoded, ok := tags.(string)
		if !ok {
			return false, errors.New("tags must be a string")
		}

		return matchPerm(attr, agents, tagsEncoded)
	})

	enforcer.AddFunction("matchResourceSelector", func(args ...any) (any, error) {
		if len(args) != 2 {
			return false, fmt.Errorf("matchResourceSelector needs 2 arguments. got %d", len(args))
		}

		attributeSet := args[0]

		if _, ok := attributeSet.(string); ok {
			return false, nil
		}

		attr, ok := attributeSet.(*models.ABACAttribute)
		if !ok {
			return false, fmt.Errorf("[matchResourceSelector] unknown input type: %T. expected *models.ABACAttribute", attributeSet)
		}

		selector, ok := args[1].(string)
		if !ok {
			return false, fmt.Errorf("[matchResourceSelector] selector must be a string")
		}

		rs, err := base64.StdEncoding.DecodeString(selector)
		if err != nil {
			return false, err
		}

		var objectSelector v1.PermissionObject
		if err := json.Unmarshal([]byte(rs), &objectSelector); err != nil {
			return false, err
		}

		var resourcesMatched int

		for _, rs := range objectSelector.Components {
			if rs.Matches(attr.Component) {
				resourcesMatched++
				break
			}
		}

		for _, rs := range objectSelector.Playbooks {
			if rs.Matches(&attr.Playbook) {
				resourcesMatched++
				break
			}
		}

		for _, rs := range objectSelector.Configs {
			if rs.Matches(attr.Config) {
				resourcesMatched++
				break
			}
		}

		return resourcesMatched == objectSelector.RequiredMatchCount(), nil
	})
}

// mapContains returns true if `request` fully contains `want`.
func mapContains(want map[string]string, request map[string]string) bool {
	for k, v := range want {
		if request[k] != v {
			return false
		}
	}

	return true
}
