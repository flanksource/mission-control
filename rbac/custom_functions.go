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
	"github.com/samber/lo"
)

func matchPerm(obj any, _agents any, tagsEncoded string) (bool, error) {
	var rObj map[string]any
	switch v := obj.(type) {
	case string:
		// an object is required to satisfy the agents & tags requirement.
		return false, nil

	case map[string]any:
		rObj = v
	}

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
	if config, ok := rObj["config"]; ok {
		var (
			tagsmatch   = true
			agentsMatch = true
		)

		tagsRaw := config.(map[string]any)["tags"]
		if tags, ok := tagsRaw.(map[string]any); ok {
			tagsmatch = mapContains(rTags, tags)
		}

		if len(rAgents) > 0 {
			agentIDRaw := config.(map[string]any)["agent_id"]
			if agentID, ok := agentIDRaw.(string); ok {
				agentsMatch = lo.Contains(rAgents, agentID)
			}
		}

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
		agents := args[1]
		tags := args[2]

		tagsEncoded, ok := tags.(string)
		if !ok {
			return false, errors.New("tags must be a string")
		}

		return matchPerm(obj, agents, tagsEncoded)
	})

	enforcer.AddFunction("matchResourceSelector", func(args ...any) (any, error) {
		if len(args) != 2 {
			return false, fmt.Errorf("matchResourceSelector needs 2 arguments. got %d", len(args))
		}

		attributeSet := args[0]
		selector := args[1]

		if _, ok := attributeSet.(string); ok {
			return false, nil
		}

		attr, ok := attributeSet.(*models.RBACAttribute)
		if !ok {
			return false, fmt.Errorf("unknown input type: %T", attributeSet)
		}

		rs, err := base64.StdEncoding.DecodeString(selector.(string))
		if err != nil {
			return false, err
		}

		var objectSelector v1.PermissionObject
		if err := json.Unmarshal([]byte(rs), &objectSelector); err != nil {
			return false, err
		}

		var resourcesMatched int

		if attr.Component != nil {
			for _, rs := range objectSelector.Components {
				if rs.Matches(attr.Component) {
					resourcesMatched++
					break
				}
			}
		}

		if attr.Playbook != nil {
			for _, rs := range objectSelector.Playbooks {
				if rs.Matches(attr.Playbook) {
					resourcesMatched++
					break
				}
			}
		}

		if attr.Config != nil {
			for _, rs := range objectSelector.Configs {
				if rs.Matches(attr.Config) {
					resourcesMatched++
					break
				}
			}
		}

		return resourcesMatched == objectSelector.RequiredMatchCount(), nil
	})
}

// mapContains returns true if `request` fully contains `want`.
func mapContains(want map[string]string, request map[string]any) bool {
	for k, v := range want {
		if request[k] != v {
			return false
		}
	}

	return true
}
