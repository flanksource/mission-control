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

	dutyRBAC "github.com/flanksource/duty/rbac"

	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/types"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/auth"
	pluginAPI "github.com/flanksource/incident-commander/plugin/api"
	"github.com/google/uuid"

	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ktypes "k8s.io/apimachinery/pkg/types"
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

func applyUserPermissions(configID string) {
	permissions := []*v1.Permission{
		permissionCRD("good-config-read", goodUser.Email, []string{policy.ActionRead}, configID),
		permissionCRD("good-plugin-invoke", goodUser.Email, []string{policy.NewPluginInvokeAction(pluginName, pluginOperation)}, configID),
		permissionCRD("no-invoke-config-read", noInvokeUser.Email, []string{policy.ActionRead}, configID),
		permissionCRD("no-config-plugin-invoke", noConfigUser.Email, []string{policy.NewPluginInvokeAction(pluginName, pluginOperation)}, configID),
	}
	for _, permission := range permissions {
		Expect(k8sClient.Create(context.Background(), permission)).To(Succeed())
	}
}

func permissionCRD(name, email string, actions []string, configID string) *v1.Permission {
	return &v1.Permission{
		TypeMeta: metav1.TypeMeta{APIVersion: "mission-control.flanksource.com/v1", Kind: "Permission"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: pluginNamespace,
			UID:       ktypes.UID(uuid.NewString()),
		},
		Spec: v1.PermissionSpec{
			Subject: v1.PermissionSubject{Person: email},
			Actions: actions,
			Object: v1.PermissionObject{Selectors: dutyRBAC.Selectors{
				Configs: []types.ResourceSelector{{ID: configID}},
			}},
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

// waitForUserPermissions waits for the permissions to take effect: they're reconciled, then the policy is reloaded.
func waitForUserPermissions(configID string) {
	path := fmt.Sprintf("/api/plugins/%s/invoke/%s?config_id=%s", pluginName, pluginOperation, configID)
	Eventually(func(g Gomega) {
		resp := doPluginRequest(http.MethodPost, path, []byte(`{}`), goodUser.Email, "test-password")
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
