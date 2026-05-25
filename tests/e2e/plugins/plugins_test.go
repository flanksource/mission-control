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
	"github.com/flanksource/duty/tests/fixtures/dummy"
	"github.com/flanksource/duty/types"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/auth"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ktypes "k8s.io/apimachinery/pkg/types"
)

const (
	pluginNamespace = "default"
	pluginName      = "hasher"
	pluginOperation = "sha256"
)

type hashResponse struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

var _ = ginkgo.Describe("Plugins E2E", ginkgo.Ordered, func() {
	config := dummy.LogisticsAPIPodConfig

	ginkgo.BeforeAll(func() {
		applyHasherPlugin()
		applyUserPermissions(config.ID.String())
		waitForHasherPlugin(config.ID.String())
	})

	ginkgo.It("authorizes /invoke and /proxy plugin operations", func() {
		expected := expectedHash(*config.Name)

		for _, endpoint := range []struct {
			name   string
			method string
			path   string
			body   []byte
		}{
			{name: "invoke", method: http.MethodPost, path: fmt.Sprintf("/api/plugins/%s/invoke/%s?config_id=%s", pluginName, pluginOperation, config.ID), body: []byte(`{}`)},
			{name: "proxy", method: http.MethodGet, path: fmt.Sprintf("/api/plugins/%s/proxy/%s?config_id=%s", pluginName, pluginOperation, config.ID)},
		} {
			ginkgo.By(endpoint.name + " allows user with config read and plugin invoke")
			resp := doPluginRequest(endpoint.method, endpoint.path, endpoint.body, goodUser.Email, "test-password")
			Expect(resp.StatusCode).To(Equal(http.StatusOK), resp.Body)
			var payload hashResponse
			Expect(json.Unmarshal([]byte(resp.Body), &payload)).To(Succeed())
			Expect(payload).To(Equal(hashResponse{Name: *config.Name, SHA256: expected}))

			ginkgo.By(endpoint.name + " rejects user without plugin invoke")
			resp = doPluginRequest(endpoint.method, endpoint.path, endpoint.body, noInvokeUser.Email, "test-password")
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden), resp.Body)

			ginkgo.By(endpoint.name + " rejects user without config read")
			resp = doPluginRequest(endpoint.method, endpoint.path, endpoint.body, noConfigUser.Email, "test-password")
			Expect(resp.StatusCode).ToNot(Equal(http.StatusOK), resp.Body)

			ginkgo.By(endpoint.name + " rejects invalid credentials")
			resp = doPluginRequest(endpoint.method, endpoint.path, endpoint.body, goodUser.Email, "wrong-password")
			Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized), resp.Body)
		}
	})
})

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

type pluginHTTPResponse struct {
	StatusCode int
	Body       string
}

func doPluginRequest(method, path string, body []byte, username, password string) pluginHTTPResponse {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, serverURL+path, reader)
	Expect(err).ToNot(HaveOccurred())
	req.SetBasicAuth(username, password)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

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
