package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/mark3labs/mcp-go/mcp"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
)

var jsonHeader = http.Header{echo.HeaderContentType: []string{echo.MIMEApplicationJSON}}

func checkResultInMCPResponse(toolResult any, results []string) {
	Expect(toolResult).ToNot(BeNil())
	content := fmt.Sprint(toolResult)
	for _, result := range results {
		Expect(content).To(ContainSubstring(result))
	}
}

func checkResultNotInMCPResponse(toolResult any, results []string) {
	Expect(toolResult).ToNot(BeNil())
	content := fmt.Sprint(toolResult)
	for _, result := range results {
		Expect(content).ToNot(ContainSubstring(result))
	}
}

var _ = ginkgo.Describe("MCP Tools", ginkgo.FlakeAttempts(3), func() {
	ginkgo.Describe("Health Check Tools", func() {
		ginkgo.It("should list all health checks", func() {
			result, err := mcpClient.CallTool(DefaultContext, mcp.CallToolRequest{
				Header: jsonHeader,
				Params: mcp.CallToolParams{
					Name: "list_all_checks",
				},
			})

			Expect(err).To(BeNil())
			ids := []string{
				dummy.LogisticsAPIHomeHTTPCheck.ID.String(),
				dummy.LogisticsAPIHealthHTTPCheck.ID.String(),
				dummy.LogisticsDBCheck.ID.String(),
			}
			checkResultInMCPResponse(result.Content, ids)
		})

		ginkgo.It("should search health checks", func() {
			result, err := mcpClient.CallTool(DefaultContext, mcp.CallToolRequest{
				Header: jsonHeader,
				Params: mcp.CallToolParams{
					Name: "search_health_checks",
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
				Header: jsonHeader,
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
			content := lo.FirstOrEmpty(result.Content)
			jsonTxt, ok := content.(mcp.TextContent)
			Expect(ok).To(BeTrue())
			var checkstatus []models.CheckStatus
			err = json.Unmarshal([]byte(jsonTxt.Text), &checkstatus)
			Expect(err).NotTo(HaveOccurred())
			var trueCount, falseCount int
			for _, c := range checkstatus {
				if c.Status {
					trueCount++
				} else {
					falseCount++
				}
			}

			Expect(trueCount).To(Equal(8))
			Expect(falseCount).To(Equal(2))
		})
	})

	ginkgo.Describe("Catalog Tools", func() {
		ginkgo.It("should list catalog types", func() {
			result, err := mcpClient.CallTool(context.Background(), mcp.CallToolRequest{
				Header: jsonHeader,
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
			result, err := mcpClient.CallTool(DefaultContext, mcp.CallToolRequest{
				Header: jsonHeader,
				Params: mcp.CallToolParams{
					Name: "search_catalog",
					Arguments: map[string]any{
						"query": "type=Kubernetes::*",
						"limit": 50,
					},
				},
			})

			Expect(err).To(BeNil())
			Expect(result.Content).NotTo(BeEmpty())

			var ids []string
			for _, config := range dummy.AllDummyConfigs {
				if config.ID != uuid.Nil {
					// There are dummy configs from different agents that search_catalog will not return
					continue
				}

				if strings.HasPrefix(lo.FromPtr(config.Type), "Kubernetes::") {
					ids = append(ids, config.ID.String())
				}
			}

			checkResultInMCPResponse(result.Content, ids)
		})

		ginkgo.It("should search catalog changes", func() {
			result, err := mcpClient.CallTool(DefaultContext, mcp.CallToolRequest{
				Header: jsonHeader,
				Params: mcp.CallToolParams{
					Name: "search_catalog_changes",
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
				Header: jsonHeader,
				Params: mcp.CallToolParams{
					Name: "get_related_configs",
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
		ginkgo.It("should get recent playbook runs", func() {
			// TODO: Add playbook run fixtures
			result, err := mcpClient.CallTool(DefaultContext, mcp.CallToolRequest{
				Header: jsonHeader,
				Params: mcp.CallToolParams{
					Name: toolGetRecentPlaybookRuns,
				},
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(result.Content).NotTo(BeEmpty())
		})

		ginkgo.It("should get failed playbook runs", func() {
			// TODO: Add playbook run fixtures
			result, err := mcpClient.CallTool(DefaultContext, mcp.CallToolRequest{
				Header: jsonHeader,
				Params: mcp.CallToolParams{
					Name: toolGetFailedPlaybookRuns,
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
				Header: jsonHeader,
				Params: mcp.CallToolParams{
					Name: "list_connections",
				},
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(result.Content).NotTo(BeEmpty())
		})
	})

	ginkgo.Describe("View Tools", func() {
		ginkgo.It("should handle view run handler correctly", func() {
			ginkgo.By("Testing view run handler by checking if it handles tool name correctly")

			// Test that viewRunHandler handles missing tools properly
			_, err := mcpClient.CallTool(DefaultContext, mcp.CallToolRequest{
				Header: jsonHeader,
				Params: mcp.CallToolParams{
					Name: "view_mission-control_default",
				},
			})

			// Should get an error for non-existent view tool
			Expect(err).To(Not(BeNil()))
		})
	})

	ginkgo.Describe("Access Tools", func() {
		ginkgo.It("should search catalog access mapping", func() {
			result, err := mcpClient.CallTool(DefaultContext, mcp.CallToolRequest{
				Header: jsonHeader,
				Params: mcp.CallToolParams{
					Name: toolSearchCatalogAccessMapping,
					Arguments: map[string]any{
						"query": "type=Kubernetes::*",
						"limit": 10,
					},
				},
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(result.IsError).To(BeFalse())
		})

		ginkgo.It("should return error when query is missing for access mapping", func() {
			result, err := mcpClient.CallTool(DefaultContext, mcp.CallToolRequest{
				Header: jsonHeader,
				Params: mcp.CallToolParams{
					Name: toolSearchCatalogAccessMapping,
				},
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(result.IsError).To(BeTrue())
		})

		ginkgo.It("should search catalog access log", func() {
			testConfigID := dummy.EKSCluster.ID.String()
			result, err := mcpClient.CallTool(DefaultContext, mcp.CallToolRequest{
				Header: jsonHeader,
				Params: mcp.CallToolParams{
					Name: toolSearchCatalogAccessLog,
					Arguments: map[string]any{
						"config_id": testConfigID,
					},
				},
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(result.IsError).To(BeFalse())
		})

		ginkgo.It("should return error for invalid config_id in access log", func() {
			result, err := mcpClient.CallTool(DefaultContext, mcp.CallToolRequest{
				Header: jsonHeader,
				Params: mcp.CallToolParams{
					Name: toolSearchCatalogAccessLog,
					Arguments: map[string]any{
						"config_id": "not-a-uuid",
					},
				},
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(result.IsError).To(BeTrue())
		})

		ginkgo.It("should search catalog access log with user_id filter", func() {
			testConfigID := dummy.EKSCluster.ID.String()
			result, err := mcpClient.CallTool(DefaultContext, mcp.CallToolRequest{
				Header: jsonHeader,
				Params: mcp.CallToolParams{
					Name: toolSearchCatalogAccessLog,
					Arguments: map[string]any{
						"config_id": testConfigID,
						"user_id":   uuid.New().String(),
					},
				},
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(result.IsError).To(BeFalse())
		})

		ginkgo.It("should return error for invalid user_id in access log", func() {
			testConfigID := dummy.EKSCluster.ID.String()
			result, err := mcpClient.CallTool(DefaultContext, mcp.CallToolRequest{
				Header: jsonHeader,
				Params: mcp.CallToolParams{
					Name: toolSearchCatalogAccessLog,
					Arguments: map[string]any{
						"config_id": testConfigID,
						"user_id":   "not-a-uuid",
					},
				},
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(result.IsError).To(BeTrue())
		})

		ginkgo.It("should return error when config_id is missing for access log", func() {
			result, err := mcpClient.CallTool(DefaultContext, mcp.CallToolRequest{
				Header: jsonHeader,
				Params: mcp.CallToolParams{
					Name: toolSearchCatalogAccessLog,
				},
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(result.IsError).To(BeTrue())
		})

		ginkgo.It("should search catalog access reviews", func() {
			result, err := mcpClient.CallTool(DefaultContext, mcp.CallToolRequest{
				Header: jsonHeader,
				Params: mcp.CallToolParams{
					Name: toolSearchCatalogAccessReviews,
					Arguments: map[string]any{
						"since": "30d",
						"limit": 10,
					},
				},
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())
		})

		ginkgo.It("should search catalog access reviews with config_id", func() {
			testConfigID := dummy.EKSCluster.ID.String()
			result, err := mcpClient.CallTool(DefaultContext, mcp.CallToolRequest{
				Header: jsonHeader,
				Params: mcp.CallToolParams{
					Name: toolSearchCatalogAccessReviews,
					Arguments: map[string]any{
						"config_id": testConfigID,
						"since":     "7d",
					},
				},
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())
		})

		ginkgo.It("should return error for invalid since duration", func() {
			result, err := mcpClient.CallTool(DefaultContext, mcp.CallToolRequest{
				Header: jsonHeader,
				Params: mcp.CallToolParams{
					Name: toolSearchCatalogAccessReviews,
					Arguments: map[string]any{
						"since": "invalid",
					},
				},
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(result.IsError).To(BeTrue())
		})
	})

	ginkgo.Describe("Resolve Tools", func() {
		ginkgo.It("should resolve config by name", func() {
			result, err := mcpClient.CallTool(DefaultContext, mcp.CallToolRequest{
				Header: jsonHeader,
				Params: mcp.CallToolParams{
					Name: toolResolveConfig,
					Arguments: map[string]any{
						"query": "Production EKS",
					},
				},
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(result.IsError).To(BeFalse())
			checkResultInMCPResponse(result.Content, []string{dummy.EKSCluster.ID.String()})
		})

		ginkgo.It("should resolve config by partial name", func() {
			result, err := mcpClient.CallTool(DefaultContext, mcp.CallToolRequest{
				Header: jsonHeader,
				Params: mcp.CallToolParams{
					Name: toolResolveConfig,
					Arguments: map[string]any{
						"query": "Production",
					},
				},
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(result.IsError).To(BeFalse())
			checkResultInMCPResponse(result.Content, []string{dummy.EKSCluster.ID.String()})
		})

		ginkgo.It("should resolve config by ID", func() {
			result, err := mcpClient.CallTool(DefaultContext, mcp.CallToolRequest{
				Header: jsonHeader,
				Params: mcp.CallToolParams{
					Name: toolResolveConfig,
					Arguments: map[string]any{
						"query": dummy.EKSCluster.ID.String(),
					},
				},
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(result.IsError).To(BeFalse())
			checkResultInMCPResponse(result.Content, []string{dummy.EKSCluster.ID.String()})
		})

		ginkgo.It("should resolve config with type filter", func() {
			result, err := mcpClient.CallTool(DefaultContext, mcp.CallToolRequest{
				Header: jsonHeader,
				Params: mcp.CallToolParams{
					Name: toolResolveConfig,
					Arguments: map[string]any{
						"query": "Production",
						"type":  "EKS::Cluster",
					},
				},
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(result.IsError).To(BeFalse())
			checkResultInMCPResponse(result.Content, []string{dummy.EKSCluster.ID.String()})
		})

		ginkgo.It("should return error when query is missing for resolve config", func() {
			result, err := mcpClient.CallTool(DefaultContext, mcp.CallToolRequest{
				Header: jsonHeader,
				Params: mcp.CallToolParams{
					Name: toolResolveConfig,
				},
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(result.IsError).To(BeTrue())
		})

		ginkgo.It("should resolve external user without error", func() {
			result, err := mcpClient.CallTool(DefaultContext, mcp.CallToolRequest{
				Header: jsonHeader,
				Params: mcp.CallToolParams{
					Name: toolResolveExternalUser,
					Arguments: map[string]any{
						"query": "nonexistent-user",
					},
				},
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(result.IsError).To(BeFalse())
		})

		ginkgo.It("should return error when query is missing for resolve external user", func() {
			result, err := mcpClient.CallTool(DefaultContext, mcp.CallToolRequest{
				Header: jsonHeader,
				Params: mcp.CallToolParams{
					Name: toolResolveExternalUser,
				},
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(result.IsError).To(BeTrue())
		})

		ginkgo.It("should resolve external group without error", func() {
			result, err := mcpClient.CallTool(DefaultContext, mcp.CallToolRequest{
				Header: jsonHeader,
				Params: mcp.CallToolParams{
					Name: toolResolveExternalGroup,
					Arguments: map[string]any{
						"query": "nonexistent-group",
					},
				},
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(result.IsError).To(BeFalse())
		})

		ginkgo.It("should return error when query is missing for resolve external group", func() {
			result, err := mcpClient.CallTool(DefaultContext, mcp.CallToolRequest{
				Header: jsonHeader,
				Params: mcp.CallToolParams{
					Name: toolResolveExternalGroup,
				},
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(result.IsError).To(BeTrue())
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
				Header: jsonHeader,
				Params: mcp.CallToolParams{
					Name:      "invalid_tool_name",
					Arguments: map[string]interface{}{},
				},
			})

			Expect(err).To(HaveOccurred())
		})

		ginkgo.It("should handle invalid parameters gracefully", func() {
			result, err := mcpClient.CallTool(DefaultContext, mcp.CallToolRequest{
				Header: jsonHeader,
				Params: mcp.CallToolParams{
					Name: "search_health_checks",
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
