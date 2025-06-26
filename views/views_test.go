package views

import (
	"time"

	"github.com/flanksource/duty/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
)

var _ = Describe("Views", func() {
	Describe("Run", func() {
		DescribeTable("queries",
			func(view v1.View, expectedRows []api.ViewRow) {
				rows, err := Run(DefaultContext, &view)
				Expect(err).ToNot(HaveOccurred())
				Expect(rows).To(Equal(expectedRows))
			},
			Entry("config queries", v1.View{
				Spec: v1.ViewSpec{
					Columns: []api.ViewColumnDef{
						{
							Name: "name",
							Type: api.ViewColumnTypeString,
						},
						{
							Name: "status",
							Type: api.ViewColumnTypeString,
						},
					},
					Queries: v1.ViewQueriesSpec{
						Configs: []v1.ViewQuery{
							{
								Selector: types.ResourceSelector{
									Types:       []string{"Kubernetes::Node"},
									TagSelector: "account=flanksource",
								},
								Max: 10,
								Mapping: map[string]types.CelExpression{
									"name":   "row.name",
									"status": "row.status",
								},
							},
						},
					},
				},
			}, []api.ViewRow{
				{"node-a", "healthy"},
				{"node-b", "healthy"},
			}),
			Entry("changes queries", v1.View{
				Spec: v1.ViewSpec{
					Columns: []api.ViewColumnDef{
						{
							Name: "name",
							Type: api.ViewColumnTypeString,
						},
						{
							Name: "status",
							Type: api.ViewColumnTypeString,
						},
					},
					Queries: v1.ViewQueriesSpec{
						Changes: []v1.ViewQuery{
							{
								Selector: types.ResourceSelector{
									Search: "change_type=CREATE",
								},
								Max: 10,
								Mapping: map[string]types.CelExpression{
									"name":   "row.name",
									"status": "row.type",
								},
							},
						},
					},
				},
			}, []api.ViewRow{
				{"Production EKS", "EKS::Cluster"},
				{"node-a", "Kubernetes::Node"},
			}),
			Entry("helm release changes queries", v1.View{
				Spec: v1.ViewSpec{
					Columns: []api.ViewColumnDef{
						{
							Name: "chart",
							Type: api.ViewColumnTypeString,
						},
						{
							Name: "version",
							Type: api.ViewColumnTypeString,
						},
						{
							Name: "source",
							Type: api.ViewColumnTypeString,
						},
					},
					Queries: v1.ViewQueriesSpec{
						Changes: []v1.ViewQuery{
							{
								Selector: types.ResourceSelector{
									Types:  []string{"Helm::Release"},
									Search: "change_type=UPDATE",
								},
								Max: 10,
								Mapping: map[string]types.CelExpression{
									"chart":   "row.name",
									"version": "row.summary.split(' to ')[1]",
									"source":  "row.source",
								},
							},
						},
					},
				},
			}, []api.ViewRow{
				{"nginx-ingress", "4.8.0", "Flux"},
				{"nginx-ingress", "4.7.2", "Flux"},
				{"nginx-ingress", "4.7.1", "Flux"},
				{"redis", "18.1.5", "Flux"},
				{"redis", "18.1.3", "Flux"},
				{"redis", "18.1.0", "Flux"},
			}),
		)
	})

	Describe("applyMapping", func() {
		type applyMappingTestCase struct {
			data     map[string]any
			columns  []api.ViewColumnDef
			mapping  map[string]types.CelExpression
			expected api.ViewRow
		}

		DescribeTable("applyMapping test cases",
			func(tc applyMappingTestCase) {
				row, err := applyMapping(tc.data, tc.columns, tc.mapping)
				Expect(err).ToNot(HaveOccurred())
				Expect(api.ViewRow(row)).To(Equal(tc.expected))
			},
			Entry("should apply CEL expressions to data", applyMappingTestCase{
				data: map[string]any{
					"name":   "test-pod",
					"status": "Running",
					"ready":  true,
				},
				mapping: map[string]types.CelExpression{
					"pod_name":   "name",
					"pod_status": "status",
				},
				columns: []api.ViewColumnDef{
					{
						Name: "pod_name",
						Type: api.ViewColumnTypeString,
					},
					{
						Name: "pod_status",
						Type: api.ViewColumnTypeString,
					},
				},
				expected: api.ViewRow{"test-pod", "Running"},
			}),
			Entry("should handle empty mapping", applyMappingTestCase{
				data: map[string]any{
					"name": "test",
				},
				mapping:  map[string]types.CelExpression{},
				expected: nil,
			}),
			Entry("should handle durations", applyMappingTestCase{
				data: map[string]any{
					"duration": "does not matter. the value is hardcoded in the mapping.",
				},
				mapping: map[string]types.CelExpression{
					"duration": "duration('1m')",
				},
				columns: []api.ViewColumnDef{
					{
						Name: "duration",
						Type: api.ViewColumnTypeDuration,
					},
				},
				expected: api.ViewRow{1 * time.Minute},
			}),
		)
	})
})
