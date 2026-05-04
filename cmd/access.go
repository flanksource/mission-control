package cmd

import (
	"fmt"
	"strings"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"

	"github.com/flanksource/incident-commander/db"
)

// parseAccessQuery converts CLI args into a ResourceSelector the same way the
// `rbac export` command does (`cmd/rbac.go`). Callers pass an empty selector
// when no args are given so the downstream query returns all configs.
func parseAccessQuery(args []string) types.ResourceSelector {
	return types.ResourceSelector{
		Cache:  "no-cache",
		Search: strings.Join(args, " "),
	}
}

// resolveExternalUserArg resolves either a UUID or a name/email substring to a
// single ExternalUser. Falls back to ResolveExternalUsers when the arg is not a
// UUID; errors if zero or more than one user matches.
func resolveExternalUserArg(ctx context.Context, arg string) (*models.ExternalUser, error) {
	if id, err := uuid.Parse(arg); err == nil {
		var u models.ExternalUser
		if err := ctx.DB().Where("id = ? AND deleted_at IS NULL", id).First(&u).Error; err != nil {
			return nil, ctx.Oops().Wrapf(err, "external user %s not found", id)
		}
		return &u, nil
	}

	users, err := db.ResolveExternalUsers(ctx, arg, 2)
	if err != nil {
		return nil, err
	}
	switch len(users) {
	case 0:
		return nil, fmt.Errorf("no external user matches %q", arg)
	case 1:
		return &users[0], nil
	default:
		names := make([]string, 0, len(users))
		for _, u := range users {
			names = append(names, u.Name)
		}
		return nil, fmt.Errorf("%q matches multiple users: %s", arg, strings.Join(names, ", "))
	}
}

// resolveExternalGroupArg resolves either a UUID or a name substring to a
// single ExternalGroup.
func resolveExternalGroupArg(ctx context.Context, arg string) (*models.ExternalGroup, error) {
	if id, err := uuid.Parse(arg); err == nil {
		var g models.ExternalGroup
		if err := ctx.DB().Where("id = ? AND deleted_at IS NULL", id).First(&g).Error; err != nil {
			return nil, ctx.Oops().Wrapf(err, "external group %s not found", id)
		}
		return &g, nil
	}

	groups, err := db.ResolveExternalGroups(ctx, arg, 2)
	if err != nil {
		return nil, err
	}
	switch len(groups) {
	case 0:
		return nil, fmt.Errorf("no external group matches %q", arg)
	case 1:
		return &groups[0], nil
	default:
		names := make([]string, 0, len(groups))
		for _, g := range groups {
			names = append(names, g.Name)
		}
		return nil, fmt.Errorf("%q matches multiple groups: %s", arg, strings.Join(names, ", "))
	}
}

// resolveExternalRoleArg resolves either a UUID or a name substring to a
// single ExternalRole.
func resolveExternalRoleArg(ctx context.Context, arg string) (*models.ExternalRole, error) {
	if id, err := uuid.Parse(arg); err == nil {
		var r models.ExternalRole
		if err := ctx.DB().Where("id = ? AND deleted_at IS NULL", id).First(&r).Error; err != nil {
			return nil, ctx.Oops().Wrapf(err, "external role %s not found", id)
		}
		return &r, nil
	}

	var roles []models.ExternalRole
	if err := ctx.DB().Table("external_roles").
		Where("deleted_at IS NULL").
		Where("name ILIKE ?", "%"+arg+"%").
		Limit(2).Find(&roles).Error; err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to resolve role %q", arg)
	}
	switch len(roles) {
	case 0:
		return nil, fmt.Errorf("no external role matches %q", arg)
	case 1:
		return &roles[0], nil
	default:
		names := make([]string, 0, len(roles))
		for _, r := range roles {
			names = append(names, r.Name)
		}
		return nil, fmt.Errorf("%q matches multiple roles: %s", arg, strings.Join(names, ", "))
	}
}
