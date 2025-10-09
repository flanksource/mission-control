package rbac

import (
	"fmt"
	"strings"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/rls"
	"github.com/google/uuid"
	"github.com/samber/lo"

	v1 "github.com/flanksource/incident-commander/api/v1"
)

// GetRLSPayloadFromScopes converts Scopes into an RLS payload for a person
func GetRLSPayloadFromScopes(ctx context.Context, personID uuid.UUID) (*rls.Payload, error) {
	scopes, err := GetScopesForPerson(ctx, personID)
	if err != nil {
		return nil, err
	}

	targets, err := ExtractTargetsFromScopes(scopes)
	if err != nil {
		return nil, err
	}

	payload := &rls.Payload{}

	for _, target := range targets {
		// Resolve agent identifiers to IDs
		var agentIDs []string
		if target.Selector.Agent != "" {
			agent, err := query.FindCachedAgent(ctx, target.Selector.Agent)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve agent %s: %w", target.Selector.Agent, err)
			} else if agent != nil {
				agentIDs = []string{agent.ID.String()}
			}
		}

		// Convert TagSelector to map
		// TagSelector format: "key1=value1,key2=value2"
		tags := parseTagSelector(target.Selector.TagSelector)

		// Build RLS scope
		rlsScope := rls.Scope{
			Tags:   tags,
			Agents: agentIDs,
		}

		// Add name if present
		if target.Selector.Name != "" {
			rlsScope.Names = []string{target.Selector.Name}
		}

		// Add to appropriate resource type in payload
		isWildcard := target.ResourceType == v1.ScopeResourceAll
		if isWildcard || target.ResourceType == v1.ScopeResourceConfig {
			payload.Config = append(payload.Config, rlsScope)
		}
		if isWildcard || target.ResourceType == v1.ScopeResourceCanary {
			payload.Canary = append(payload.Canary, rlsScope)
		}
		if isWildcard || target.ResourceType == v1.ScopeResourcePlaybook {
			payload.Playbook = append(payload.Playbook, rlsScope)
		}
		if isWildcard || target.ResourceType == v1.ScopeResourceComponent {
			payload.Component = append(payload.Component, rlsScope)
		}
	}

	return payload, nil
}

// parseTagSelector converts a tag selector string to a map
// Example: "env=prod,region=us-west" -> {"env": "prod", "region": "us-west"}
func parseTagSelector(selector string) map[string]string {
	if selector == "" {
		return nil
	}

	tags := make(map[string]string)
	pairs := lo.Map(lo.Filter(lo.Map(
		strings.Split(selector, ","),
		func(s string, _ int) string { return strings.TrimSpace(s) }),
		func(s string, _ int) bool { return s != "" }),
		func(s string, _ int) []string { return strings.Split(s, "=") })

	for _, pair := range pairs {
		if len(pair) == 2 {
			tags[strings.TrimSpace(pair[0])] = strings.TrimSpace(pair[1])
		}
	}

	return tags
}
