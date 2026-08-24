package sdk

import (
	"context"

	"github.com/flanksource/commons/collections"
	"github.com/google/uuid"
)

type identityCandidate struct {
	ID      uuid.UUID `json:"id"`
	Name    string    `json:"name"`
	Email   string    `json:"email"`
	Aliases []string  `json:"aliases"`
}

type identityMatch struct {
	table       string
	typeColumn  string
	selectQuery string
	fields      func(identityCandidate) []string
}

var (
	userIdentityMatch = identityMatch{
		table:       "external_users",
		typeColumn:  "user_type",
		selectQuery: "id,name,email,aliases",
		fields: func(candidate identityCandidate) []string {
			return append([]string{candidate.ID.String(), candidate.Name, candidate.Email}, candidate.Aliases...)
		},
	}
	groupIdentityMatch = identityMatch{
		table:       "external_group_summary",
		typeColumn:  "group_type",
		selectQuery: "id,name,aliases",
		fields: func(candidate identityCandidate) []string {
			return append([]string{candidate.ID.String(), candidate.Name}, candidate.Aliases...)
		},
	}
	roleIdentityMatch = identityMatch{
		table:       "external_roles",
		typeColumn:  "role_type",
		selectQuery: "id,name",
		fields: func(candidate identityCandidate) []string {
			return []string{candidate.ID.String(), candidate.Name}
		},
	}
)

func (c *Client) matchingIdentityIDs(ctx context.Context, opts IdentityOptions, match identityMatch) ([]string, int, error) {
	params := opts.params(match.typeColumn)
	params.Del("limit")
	params.Set("select", match.selectQuery)

	var candidates []identityCandidate
	if _, err := c.pgGet(ctx, match.table, params, &candidates); err != nil {
		return nil, 0, err
	}

	ids := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		matched, _ := collections.MatchAny(match.fields(candidate), opts.Name)
		if matched {
			ids = append(ids, candidate.ID.String())
		}
	}
	total := len(ids)
	if opts.Limit > 0 && len(ids) > opts.Limit {
		ids = ids[:opts.Limit]
	}
	return ids, total, nil
}
