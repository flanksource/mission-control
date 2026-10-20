package plugins

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/flanksource/duty/rbac/policy"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/auth"
	pluginAPI "github.com/flanksource/incident-commander/plugin/api"

	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func applyHasherPlugin() {
	plugin := &v1.Plugin{
		TypeMeta: metav1.TypeMeta{APIVersion: "mission-control.flanksource.com/v1", Kind: "Plugin"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      pluginName,
			Namespace: pluginNamespace,
		},
		Spec: v1.PluginSpec{Source: pluginName},
	}
	Expect(k8sClient.Create(context.Background(), plugin)).To(Succeed())
}

// configScope is the Scope of the config the plugin is invoked on. Roles name it, rather than selecting the config
// inline, so that it's also listed under row-level security, which the plugin's config lookups run under.
const configScope = "plugin-config"

func applyUserPermissions(configID string) {
	scope := &v1.Scope{
		TypeMeta:   metav1.TypeMeta{APIVersion: "mission-control.flanksource.com/v1", Kind: "Scope"},
		ObjectMeta: metav1.ObjectMeta{Name: configScope, Namespace: pluginNamespace},
		Spec: v1.ScopeSpec{Targets: []v1.ScopeTarget{{
			Config: &v1.ScopeConfigSelector{ID: configID},
		}}},
	}
	Expect(k8sClient.Create(context.Background(), scope)).To(Succeed())

	objects := []client.Object{
		roleCRD("config-read", policy.ActionRead),
		roleCRD("plugin-invoke", policy.NewPluginInvokeAction(pluginName, pluginOperation)),
		roleBindingCRD("config-read", goodUser.Email, noInvokeUser.Email),
		roleBindingCRD("plugin-invoke", goodUser.Email, noConfigUser.Email),
	}
	for _, object := range objects {
		Expect(k8sClient.Create(context.Background(), object)).To(Succeed())
	}
}

// roleCRD is a Role named after its one rule, which allows the action on the config of configScope.
func roleCRD(name, action string) *v1.Role {
	return &v1.Role{
		TypeMeta:   metav1.TypeMeta{APIVersion: "mission-control.flanksource.com/v1", Kind: "Role"},
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: pluginNamespace},
		Spec: v1.RoleSpec{Rules: []v1.RoleRule{{
			Name:     name,
			Action:   action,
			Resource: &v1.ScopeReference{ScopeRef: configScope},
		}}},
	}
}

// roleBindingCRD binds the people to the Role of the same name.
func roleBindingCRD(role string, emails ...string) *v1.RoleBinding {
	return &v1.RoleBinding{
		TypeMeta:   metav1.TypeMeta{APIVersion: "mission-control.flanksource.com/v1", Kind: "RoleBinding"},
		ObjectMeta: metav1.ObjectMeta{Name: role, Namespace: pluginNamespace},
		Spec: v1.RoleBindingSpec{
			Role:     role,
			Subjects: v1.RoleBindingSubjects{PermissionGroupSubjects: v1.PermissionGroupSubjects{People: emails}},
		},
	}
}

func waitForHasherPlugin(configID string) {
	Eventually(func(g Gomega) {
		resp := doPluginRequest(http.MethodGet, fmt.Sprintf("/api/plugins?config_id=%s", configID), nil, auth.AdminEmail, auth.DefaultAdminPassword)
		g.Expect(resp.StatusCode).To(Equal(http.StatusOK), resp.Body)
		g.Expect(resp.Body).To(ContainSubstring(pluginName))
		g.Expect(resp.Body).To(ContainSubstring(pluginOperation))
	}).WithTimeout(90 * time.Second).WithPolling(time.Second).Should(Succeed())
}

// waitForUserPermissions waits for the Roles and RoleBindings to take effect: they're reconciled, then the policy is reloaded.
func waitForUserPermissions(configID string) {
	path := fmt.Sprintf("/api/plugins/%s/invoke/%s?config_id=%s", pluginName, pluginOperation, configID)
	Eventually(func(g Gomega) {
		resp := doPluginRequest(http.MethodPost, path, []byte(`{}`), goodUser.Email, "test-password")
		g.Expect(resp.StatusCode).To(Equal(http.StatusOK), resp.Body)

		// Each object can land in a different debounced policy reload, so goodUser's grants aren't proof that noInvokeUser's are loaded.
		resp, _ = mintUIToken(noInvokeUser.Email, configID)
		g.Expect(resp.StatusCode).To(Equal(http.StatusOK), resp.Body)
	}).WithTimeout(30 * time.Second).WithPolling(500 * time.Millisecond).Should(Succeed())
}

type pluginHTTPResponse struct {
	StatusCode int
	Body       string
}

func doPluginRequest(method, path string, body []byte, username, password string) pluginHTTPResponse {
	req := newPluginRequest(method, path, body)
	req.SetBasicAuth(username, password)
	return sendPluginRequest(req)
}

// doPluginTokenRequest sends a request authenticated only by a plugin invocation token, as a plugin UI does.
func doPluginTokenRequest(method, path string, body []byte, token string) pluginHTTPResponse {
	req := newPluginRequest(method, path, body)
	req.Header.Set(pluginAPI.InvocationTokenHTTPHeader, token)
	return sendPluginRequest(req)
}

// mintUIToken requests a plugin UI invocation token for the config as the given user.
func mintUIToken(username, configID string) (pluginHTTPResponse, string) {
	resp := doPluginRequest(http.MethodGet, fmt.Sprintf("/api/plugins/%s/ui-token?config_id=%s", pluginName, configID), nil, username, "test-password")
	if resp.StatusCode != http.StatusOK {
		return resp, ""
	}

	var payload struct {
		Token string `json:"token"`
	}
	Expect(json.Unmarshal([]byte(resp.Body), &payload)).To(Succeed())
	Expect(payload.Token).ToNot(BeEmpty())
	return resp, payload.Token
}

func newPluginRequest(method, path string, body []byte) *http.Request {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, serverURL+path, reader)
	Expect(err).ToNot(HaveOccurred())
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req
}

func sendPluginRequest(req *http.Request) pluginHTTPResponse {
	resp, err := http.DefaultClient.Do(req)
	Expect(err).ToNot(HaveOccurred())
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	Expect(err).ToNot(HaveOccurred())
	return pluginHTTPResponse{StatusCode: resp.StatusCode, Body: string(respBody)}
}

func expectedHash(name string) string {
	sum := sha256.Sum256([]byte(name))
	return hex.EncodeToString(sum[:])
}
