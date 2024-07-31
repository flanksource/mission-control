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

	"github.com/flanksource/commons/rand"
	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/labstack/echo/v4"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/rbac"
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

func DownloadKubeConfig(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	parsed, err := url.Parse(api.PublicURL)
	if err != nil {
		return fmt.Errorf("failed to parse public web url")
	}
	parsed.Path = path.Join(parsed.Path, "kubeproxy")

	seed, err := rand.GenerateRandHex(32)
	if err != nil {
		return err
	}

	token, err := db.CreateAccessToken(ctx, ctx.User().ID, ctx.User().Email, seed, nil)
	if err != nil {
		return fmt.Errorf("failed to create a new access token: %w", err)
	}

	// TODO: use the actual tenant slug once we pass it on from the chart
	tenantSlug := strings.TrimSuffix(parsed.Hostname(), ".flanksource.com")
	kcd := kubeConfigData{
		Server:      parsed.String(),
		ClusterName: fmt.Sprintf("mission-control-tenant-%s", tenantSlug),
		ContextName: "default",
		Password:    token,
	}

	tmpl, err := template.New("kubeconfig").Parse(kubeConfigTemplate)
	if err != nil {
		return fmt.Errorf("failed to parse kubeconfig template: %w", err)
	}

	var kubeconfig bytes.Buffer
	err = tmpl.Execute(&kubeconfig, kcd)
	if err != nil {
		return fmt.Errorf("failed to execute kubeconfig template")
	}

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
		if rbac.CheckContext(ctx, rbac.ObjectKubernetesProxy, rbac.ActionUpdate) {
			impersonateUser = "mission-control-writer"
		} else if rbac.CheckContext(ctx, rbac.ObjectKubernetesProxy, rbac.ActionRead) {
			impersonateUser = "mission-control-reader"
		} else {
			return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EFORBIDDEN, "user cannot impersonate mission-control reader or writer"))
		}
		c.Request().Header.Set("Impersonate-User", impersonateUser)

		c.Request().Header.Set(echo.HeaderAuthorization, fmt.Sprintf("Bearer %s", strings.TrimSpace(string(saToken))))
		return next(c)
	}
}
