package rbac

import (
	"errors"
	"fmt"

	"github.com/casbin/govaluate"
	"github.com/flanksource/commons/collections"
)

func matchPerm(obj any, _agents any, tagsEncoded string) (bool, error) {
	var rObj map[string]any
	switch v := obj.(type) {
	case string:
		return true, nil

	case map[string]any:
		rObj = v
	}

	var rAgents []any
	switch v := _agents.(type) {
	case []any:
		rAgents = v
	case string:
		if v != "" {
			rAgents = append(rAgents, v)
		}
	}

	rTags := collections.SelectorToMap(tagsEncoded)
	if config, ok := rObj["config"]; ok {
		var tagsmatch = len(rTags) == 0
		var agentsMatch = len(rAgents) == 0

		// All tags must match
		tagsRaw := config.(map[string]any)["tags"]
		if tags, ok := tagsRaw.(map[string]any); ok {
			for k, v := range rTags {
				if tags[k] != v {
					tagsmatch = false
					break
				}
			}

			tagsmatch = true
		}

		// Any agent must match
		agentIDRaw := config.(map[string]any)["agent_id"]
		if agentID, ok := agentIDRaw.(string); ok {
			for _, id := range rAgents {
				if agentID == id {
					agentsMatch = true
					break
				}
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
}
