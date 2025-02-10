package connection

import (
	"fmt"

	"github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/rbac"
	"github.com/flanksource/incident-commander/rbac/policy"
	"github.com/samber/oops"
)

func GetConection(ctx context.Context, connectionName string) (*models.Connection, error) {
	connection, err := ctx.HydrateConnectionByURL(connectionName)
	if err != nil {
		return nil, err
	} else if connection == nil {
		return nil, fmt.Errorf("connection (%s) not found", connectionName)
	}

	attr := models.ABACAttribute{Connection: *connection}
	if !rbac.HasPermission(ctx, ctx.Subject(), &attr, policy.ActionRead) {
		return nil, oops.Code(api.EUNAUTHORIZED).Wrapf(err, "unauthorized")
	}

	return connection, nil
}
