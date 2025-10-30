package mcp

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
)

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

var _ = ginkgo.Describe("MCP Tools", func() {
	ginkgo.Describe("Health Check Tools", func() {
		// TODO: Remove skip once mcp-go v0.43.0+ is released with streamable HTTP fixes
		// These tests fail intermittently in v0.42.0 due to "unexpected nil response" bug
		// See: https://github.com/mark3labs/mcp-go/issues/447
		ginkgo.It("should list all health checks", ginkgo.FlakeAttempts(3), func() {
			result, err := mcpClient.CallTool(DefaultContext, mcp.CallToolRequest{
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

		ginkgo.It("should search health checks", ginkgo.FlakeAttempts(3), func() {
			result, err := mcpClient.CallTool(DefaultContext, mcp.CallToolRequest{
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

		ginkgo.It("should get check status", ginkgo.FlakeAttempts(3), func() {
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
			contentLines := strings.Split(fmt.Sprint(result.Content), "\n")
			statusIndex := slices.Index(strings.Split(contentLines[0], " | "), "status")
			var trueCount, falseCount int
			for _, line := range contentLines[1:] {
				checkStatusData := strings.Split(line, " | ")
				if len(checkStatusData) > statusIndex {
					status := strings.TrimSpace(checkStatusData[statusIndex-1])
					if status == "true" {
						trueCount++
					}
					if status == "false" {
						falseCount++
					}
				}
			}
			Expect(trueCount).To(Equal(8))
			Expect(falseCount).To(Equal(2))
		})
	})

	ginkgo.Describe("Catalog Tools", func() {
		// TODO: Remove FlakeAttempts once mcp-go v0.43.0+ is released with streamable HTTP fixes
		// These tests fail intermittently in v0.42.0 due to "unexpected nil response" bug
		// See: https://github.com/mark3labs/mcp-go/issues/447
		ginkgo.It("should list catalog types", ginkgo.FlakeAttempts(3), func() {
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

		// TODO: Remove FlakeAttempts once mcp-go v0.43.0+ is released with streamable HTTP fixes
		// This test fails intermittently in v0.42.0 due to "unexpected nil response" bug
		// See: https://github.com/mark3labs/mcp-go/issues/447
		ginkgo.It("should search catalog", ginkgo.FlakeAttempts(3), func() {
			result, err := mcpClient.CallTool(DefaultContext, mcp.CallToolRequest{
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

		ginkgo.It("should search catalog changes", ginkgo.FlakeAttempts(3), func() {
			result, err := mcpClient.CallTool(DefaultContext, mcp.CallToolRequest{
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

		ginkgo.It("should get related configs", ginkgo.FlakeAttempts(3), func() {
			testConfigID := dummy.LogisticsAPIDeployment.ID.String()
			result, err := mcpClient.CallTool(DefaultContext, mcp.CallToolRequest{
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
					Name: "list_all_views",
				},
			})

			Expect(err).NotTo(HaveOccurred())
			checkResultInMCPResponse(result.Content, []string{
				generateViewToolName(dummy.PodView),
				generateViewToolName(dummy.ViewDev),
			})
		})

		ginkgo.It("should handle view run handler correctly", func() {
			ginkgo.By("Testing view run handler by checking if it handles tool name correctly")

			// Test that viewRunHandler handles missing tools properly
			_, err := mcpClient.CallTool(DefaultContext, mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name: "view_mission-control_default",
				},
			})

			// Should get an error for non-existent view tool
			Expect(err).To(Not(BeNil()))
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
