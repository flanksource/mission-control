package echo

import (
	"bytes"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"

	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/labstack/echo/v4"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	icrbac "github.com/flanksource/incident-commander/rbac"
	"github.com/flanksource/incident-commander/rbac/adapter"
	"github.com/flanksource/incident-commander/rbac/shipped"
)

const kubeConfigTemplate = `apiVersion: v1
clusters:
- cluster:
    server: {{.Server}}
  name: {{.ClusterName}}
contexts:
- context:
    cluster: {{.ClusterName}}
    user: default-user
  name: {{.ContextName}}
current-context: {{.ContextName}}
users:
- name: default-user
  user:
    username: "token"
    password: "{{.Password}}"
`

type kubeConfigData struct {
	Server      string
	ClusterName string
	Password    string
	ContextName string
}

// DownloadKubeConfig returns a kubeconfig for the Kubernetes proxy. Its credential is a new access token of the
// person, limited by the shipped kubernetes-proxy Role, so it can only be used for the proxy.
func DownloadKubeConfig(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	if !icrbac.CheckContext(ctx, policy.ObjectKubernetesProxy, adapter.ActionKubernetesProxyWrite) &&
		!icrbac.CheckContext(ctx, policy.ObjectKubernetesProxy, adapter.ActionKubernetesProxyRead) {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EFORBIDDEN, "%s or %s is required to download a kubeconfig",
			adapter.ActionKubernetesProxyRead, adapter.ActionKubernetesProxyWrite))
	}

	// The kubeconfig embeds an access token, which authenticates as the person without the grants the subject is limited to
	if !icrbac.ActsAsPerson(ctx) {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EFORBIDDEN, "kubeconfigs can't be downloaded by users of external identity providers"))
	}

	parsed, err := url.Parse(api.PublicURL)
	if err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrapf(err, "failed to parse public web url"))
	}
	parsed.Path = path.Join(parsed.Path, "kubeproxy")

	token, _, err := db.CreateAccessTokenForPerson(ctx, ctx.User(), "kubeconfig", 0, false,
		db.AccessTokenRoles{{Namespace: adapter.ShippedNamespace, Name: shipped.RoleKubernetesProxy}})
	if err != nil {
		return dutyAPI.WriteError(c, err)
	}

	// TODO: use the actual tenant slug once we pass it on from the chart
	tenantSlug := strings.TrimSuffix(parsed.Hostname(), ".flanksource.com")
	kcd := kubeConfigData{
		Server:      parsed.String(),
		ClusterName: fmt.Sprintf("mission-control-%s", tenantSlug),
		ContextName: "default",
		Password:    token.PlainText(),
	}

	tmpl, err := template.New("kubeconfig").Parse(kubeConfigTemplate)
	if err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrapf(err, "failed to parse kubeconfig template"))
	}

	var kubeconfig bytes.Buffer
	if err := tmpl.Execute(&kubeconfig, kcd); err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrapf(err, "failed to execute kubeconfig template"))
	}

	// The response carries a bearer token; never let a browser or proxy cache it.
	c.Response().Header().Set("Cache-Control", "no-store")
	c.Response().Header().Add(echo.HeaderContentDisposition, fmt.Sprintf("attachment; filename=%s-kubeconfig.yaml", tenantSlug))
	return c.Stream(http.StatusOK, "text/yaml", &kubeconfig)
}

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
