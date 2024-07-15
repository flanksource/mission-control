package echo

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"html/template"
	"net/http"
	"strings"

	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/labstack/echo/v4"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/flanksource/incident-commander/api"
)

const kubeConfigTemplate = `apiVersion: v1
clusters:
- cluster:
    certificate-authority-data: |
			{{- .CAData | nindent 6}}
    server: {{.Server}}
  name: {{.ClusterName}}
contexts:
- context:
    cluster: {{.ClusterName}}
    user: {{.UserName}}
  name: {{.ContextName}}
current-context: {{.ContextName}}
users:
- name: {{.UserName}}
  user:
    token: {{.Token}}
`

type kubeConfigData struct {
	CAData      string
	Server      string
	ClusterName string
	ContextName string
	UserName    string
	Token       string
}

func DownloadKubeConfig(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	secretName := c.QueryParam("secret")
	secret, err := ctx.Kubernetes().CoreV1().Secrets(api.Namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINVALID, "failed to retrieve secret namespace=%s secret=%s", api.Namespace, secretName))
	}

	token, ok := secret.Data["token"]
	if !ok {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINVALID, "token not found in secret"))
	}

	kcd := kubeConfigData{
		CAData:      base64.StdEncoding.EncodeToString(api.KubernetesRestConfig.CAData),
		Server:      api.KubernetesRestConfig.Host,
		ClusterName: "kubernetes",
		ContextName: "default",
		UserName:    "service-account-user",
		Token:       string(token),
	}

	tmpl, err := template.New("kubeconfig").Funcs(template.FuncMap{
		"nindent": func(spaces int, v string) string {
			lines := strings.Split(strings.TrimSpace(v), "\n")
			if len(lines) == 0 {
				return ""
			}
			pad := strings.Repeat(" ", spaces)
			for i, line := range lines {
				lines[i] = pad + strings.TrimSpace(line)
			}
			return "\n" + strings.Join(lines, "\n")
		},
	}).Parse(kubeConfigTemplate)
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
