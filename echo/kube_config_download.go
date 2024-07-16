package echo

import (
	"bytes"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"os"

	"github.com/labstack/echo/v4"

	"github.com/flanksource/incident-commander/api"
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
  token: "<placeholder>"
`

type kubeConfigData struct {
	Server      string
	ClusterName string
	ContextName string
}

func DownloadKubeConfig(c echo.Context) error {
	parsed, err := url.Parse(api.PublicWebURL)
	if err != nil {
		return fmt.Errorf("failed to parse public web url")
	}
	parsed.Path = "/kube-proxy"

	kcd := kubeConfigData{
		Server:      parsed.String(),
		ClusterName: "kubernetes",
		ContextName: "default",
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
		if err != nil {
			return fmt.Errorf("failed to read service account token: %w", err)
		}

		c.Request().Header.Set("Authorization", fmt.Sprintf("Bearer %s", saToken))
		return next(c)
	}
}
