package controller

import (
	dutyAPI "github.com/flanksource/duty/api"
	dutyContext "github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/incident-commander/plugin/registry"
)

func pluginRolesForUser(ctx dutyContext.Context, entry *registry.Entry, configID string) ([]string, error) {
	user := ctx.User()
	if user == nil {
		return nil, ctx.Oops().Code(dutyAPI.EUNAUTHORIZED).Errorf("not logged in")
	}
	if entry == nil || entry.Manifest == nil {
		return nil, nil
	}

	attr, err := pluginABACAttribute(ctx, configID)
	if err != nil {
		return nil, err
	}

	var roles []string
	for _, role := range entry.Manifest.Roles {
		if role == nil || role.Name == "" {
			continue
		}
		if canAssumePluginRole(ctx, user.ID.String(), attr, entry.Name, role.Name) {
			roles = append(roles, role.Name)
		}
	}
	return roles, nil
}

func canAssumePluginRole(ctx dutyContext.Context, subject string, attr *models.ABACAttribute, pluginName, role string) bool {
	return dutyRBAC.HasPermission(ctx, subject, attr, policy.NewPluginRoleAction(pluginName, role))
}

func pluginABACAttribute(ctx dutyContext.Context, configID string) (*models.ABACAttribute, error) {
	attr := &models.ABACAttribute{}
	if configID == "" {
		return attr, nil
	}

	item, err := query.ConfigItemFromCache(ctx, configID)
	if err != nil {
		return nil, ctx.Oops().Wrapf(err, "get config item %s", configID)
	}
	attr.Config = item
	return attr, nil
}
