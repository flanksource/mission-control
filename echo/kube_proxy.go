package echo

import (
	"fmt"
	"os"
	"strings"

	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/labstack/echo/v4"

	icrbac "github.com/flanksource/incident-commander/rbac"
	"github.com/flanksource/incident-commander/rbac/adapter"
)

func KubeProxyTokenMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		path := "/var/run/secrets/kubernetes.io/serviceaccount/token"
		saToken, err := os.ReadFile(path)
		if err != nil && false {
			return fmt.Errorf("failed to read service account token: %w", err)
		}

		ctx := c.Request().Context().(context.Context)

		var impersonateUser string
		if icrbac.CheckContext(ctx, policy.ObjectKubernetesProxy, adapter.ActionKubernetesProxyWrite) {
			impersonateUser = "mission-control-writer"
		} else if icrbac.CheckContext(ctx, policy.ObjectKubernetesProxy, adapter.ActionKubernetesProxyRead) {
			impersonateUser = "mission-control-reader"
		} else {
			return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EFORBIDDEN, "%s or %s is required to use the kubernetes proxy",
				adapter.ActionKubernetesProxyRead, adapter.ActionKubernetesProxyWrite))
		}
		c.Request().Header.Set("Impersonate-User", impersonateUser)

		c.Request().Header.Set(echo.HeaderAuthorization, fmt.Sprintf("Bearer %s", strings.TrimSpace(string(saToken))))
		return next(c)
	}
}
