package plugins

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/flanksource/duty/tests/fixtures/dummy"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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
