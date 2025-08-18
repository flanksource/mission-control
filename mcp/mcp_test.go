package mcp

import (
	"context"
	"fmt"
	"strings"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/tests/fixtures/dummy"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
)

func checkResultInMCPResponse(toolResult any, results []string) {
	Expect(toolResult).ToNot(BeNil())
	content := fmt.Sprint(toolResult)
	for _, result := range results {
		inResult := strings.Contains(content, result)
		Expect(inResult).To(BeTrue(), "did not find: "+result)
	}
}

func checkResultNotInMCPResponse(toolResult any, results []string) {
	Expect(toolResult).ToNot(BeNil())
	content := fmt.Sprint(toolResult)
	for _, result := range results {
		inResult := strings.Contains(content, result)
		Expect(inResult).To(BeFalse(), "found: "+result)
	}
}

var _ = ginkgo.Describe("MCP Tools", func() {

	ginkgo.Describe("Health Check Tools", func() {
		ginkgo.It("should list all health checks", func() {
			result, err := mcpClient.CallTool(DefaultContext, mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name: "list_all_checks",
				},
			})

			Expect(err).NotTo(HaveOccurred())
			ids := []string{
				dummy.LogisticsAPIHomeHTTPCheck.ID.String(),
				dummy.LogisticsAPIHealthHTTPCheck.ID.String(),
				dummy.LogisticsDBCheck.ID.String(),
			}
			checkResultInMCPResponse(result.Content, ids)
		})

		ginkgo.It("should search health checks", func() {
			result, err := mcpClient.CallTool(DefaultContext, mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name: "health_check_search",
					Arguments: map[string]any{
						"query": "status=unhealthy",
					},
				},
			})

			Expect(err).NotTo(HaveOccurred())
			checkResultInMCPResponse(result.Content, []string{dummy.LogisticsDBCheck.ID.String()})

			healthyCheckIDs := []string{
				dummy.LogisticsAPIHomeHTTPCheck.ID.String(),
				dummy.LogisticsAPIHealthHTTPCheck.ID.String(),
			}
			checkResultNotInMCPResponse(result.Content, healthyCheckIDs)
		})

		ginkgo.It("should get check status", func() {
			testCheckID := dummy.LogisticsAPIHealthHTTPCheck.ID.String()

			result, err := mcpClient.CallTool(DefaultContext, mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name: "get_check_status",
					Arguments: map[string]any{
						"id":    testCheckID,
						"limit": 10,
					},
				},
			})

			Expect(err).NotTo(HaveOccurred())

			// As per dummy data for last 10 check_status, we should get 8 passing and 2 failing check status
			Expect(strings.Count(fmt.Sprint(result.Content), `"status":true`)).To(Equal(8))
			Expect(strings.Count(fmt.Sprint(result.Content), `"status":false`)).To(Equal(2))
		})
	})

	ginkgo.Describe("Catalog Tools", func() {
		ginkgo.It("should list catalog types", func() {
			result, err := mcpClient.CallTool(context.Background(), mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name: "list_catalog_types",
				},
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(result.Content).NotTo(BeEmpty())

			configTypes := lo.Map(dummy.AllDummyConfigs, func(c models.ConfigItem, _ int) string { return lo.FromPtr(c.Type) })
			checkResultInMCPResponse(result.Content, configTypes)
		})

		ginkgo.It("should search catalog", func() {
			result, err := mcpClient.CallTool(context.Background(), mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name: "catalog_search",
					Arguments: map[string]any{
						"query": "type=Kubernetes::*",
						"limit": 50,
					},
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Content).NotTo(BeEmpty())

			ids := lo.Map(dummy.AllDummyConfigs, func(c models.ConfigItem, _ int) string {
				if strings.HasPrefix(lo.FromPtr(c.Type), "Kubernetes::") {
					return c.ID.String()
				}
				return ""
			})
			checkResultInMCPResponse(result.Content, ids)
		})

		ginkgo.It("should search catalog changes", func() {
			result, err := mcpClient.CallTool(DefaultContext, mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name: "catalog_changes_search",
					Arguments: map[string]any{
						"query": "change_type=CREATE",
					},
				},
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(result.Content).NotTo(BeEmpty())

			ids := []string{dummy.EKSClusterCreateChange.ID, dummy.KubernetesNodeAChange.ID}
			checkResultInMCPResponse(result.Content, ids)
		})

		ginkgo.It("should get related configs", func() {
			testConfigID := dummy.LogisticsAPIDeployment.ID.String()
			result, err := mcpClient.CallTool(DefaultContext, mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name: "related_configs",
					Arguments: map[string]any{
						"id": testConfigID,
					},
				},
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(result.Content).NotTo(BeEmpty())

			ids := []string{dummy.LogisticsAPIPodConfig.ID.String(), dummy.LogisticsAPIReplicaSet.ID.String()}
			checkResultInMCPResponse(result.Content, ids)

		})
	})

	ginkgo.Describe("Playbook Tools", func() {
		ginkgo.It("should list all playbooks", func() {
			result, err := mcpClient.CallTool(DefaultContext, mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name: "playbooks_list_all",
				},
			})

			Expect(err).NotTo(HaveOccurred())
			checkResultInMCPResponse(result.Content, []string{dummy.EchoConfig.Name})

		})

		ginkgo.It("should get recent playbook runs", func() {
			// TODO: Add playbook run fixtures
			result, err := mcpClient.CallTool(DefaultContext, mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name: "playbook_recent_runs",
				},
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(result.Content).NotTo(BeEmpty())
		})

		ginkgo.It("should get failed playbook runs", func() {
			// TODO: Add playbook run fixtures
			result, err := mcpClient.CallTool(DefaultContext, mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name: "playbook_failed_runs",
				},
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(result.Content).NotTo(BeEmpty())
		})
	})

	ginkgo.Describe("Connection Tools", func() {
		ginkgo.It("should list connections", func() {
			// TODO: Add connection fixtures
			result, err := mcpClient.CallTool(DefaultContext, mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name: "list_connections",
				},
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(result.Content).NotTo(BeEmpty())
		})
	})

	ginkgo.Describe("View Tools", func() {
		ginkgo.It("should list views", func() {
			result, err := mcpClient.CallTool(DefaultContext, mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name: "list_views",
				},
			})

			Expect(err).NotTo(HaveOccurred())
			checkResultInMCPResponse(result.Content, []string{dummy.View.ID.String()})
		})
	})

	ginkgo.Describe("Resource Reading", func() {
		ginkgo.It("should read config item resource", func() {
			testConfigID := dummy.EKSCluster.ID.String()
			resourceURI := fmt.Sprintf("config_item://%s", testConfigID)

			result, err := mcpClient.ReadResource(DefaultContext, mcp.ReadResourceRequest{
				Params: mcp.ReadResourceParams{
					URI: resourceURI,
				},
			})

			Expect(err).NotTo(HaveOccurred())
			checkResultInMCPResponse(result.Contents, []string{testConfigID})
		})

		ginkgo.It("should read playbook resource", func() {
			testPlaybookID := dummy.EchoConfig.ID.String()
			resourceURI := fmt.Sprintf("playbook://%s", testPlaybookID)

			result, err := mcpClient.ReadResource(DefaultContext, mcp.ReadResourceRequest{
				Params: mcp.ReadResourceParams{
					URI: resourceURI,
				},
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(result.Contents).NotTo(BeEmpty())
			checkResultInMCPResponse(result.Contents, []string{testPlaybookID})
		})

		ginkgo.It("should read connection resource", func() {
			ginkgo.Skip("TODO: Need to add connection fixtures")
			testConnectionNamespace := "default"
			testConnectionName := "kubernetes"
			resourceURI := fmt.Sprintf("connection://%s/%s", testConnectionNamespace, testConnectionName)

			result, err := mcpClient.ReadResource(DefaultContext, mcp.ReadResourceRequest{
				Params: mcp.ReadResourceParams{
					URI: resourceURI,
				},
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(result.Contents).NotTo(BeEmpty())
		})
	})

	ginkgo.Describe("Error Handling", func() {
		ginkgo.It("should handle invalid tool names gracefully", func() {
			_, err := mcpClient.CallTool(DefaultContext, mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name:      "invalid_tool_name",
					Arguments: map[string]interface{}{},
				},
			})

			Expect(err).To(HaveOccurred())
		})

		ginkgo.It("should handle invalid parameters gracefully", func() {
			result, err := mcpClient.CallTool(DefaultContext, mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name: "health_check_search",
					Arguments: map[string]interface{}{
						// Missing required "query" parameter
						"limit": 5,
					},
				},
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(result.IsError).To(BeTrue())
		})

		ginkgo.It("should handle invalid resource URIs gracefully", func() {
			_, err := mcpClient.ReadResource(DefaultContext, mcp.ReadResourceRequest{
				Params: mcp.ReadResourceParams{
					URI: "invalid://resource/uri",
				},
			})

			Expect(err).To(HaveOccurred())
		})

		ginkgo.It("should handle non-existent resources gracefully", func() {
			nonExistentID := uuid.New().String()
			resourceURI := fmt.Sprintf("config_item://%s", nonExistentID)

			_, err := mcpClient.ReadResource(DefaultContext, mcp.ReadResourceRequest{
				Params: mcp.ReadResourceParams{
					URI: resourceURI,
				},
			})

			Expect(err).To(HaveOccurred())
		})
	})
})
