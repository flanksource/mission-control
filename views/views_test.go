package views

import (
	"time"

	"github.com/flanksource/duty/types"
	pkgView "github.com/flanksource/duty/view"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1 "github.com/flanksource/incident-commander/api/v1"
)

var _ = Describe("Views", func() {
	Describe("Run", func() {
		DescribeTable("queries",
			func(view v1.View, expectedRows []pkgView.Row) {
				result, err := Run(DefaultContext, &view)
				Expect(err).ToNot(HaveOccurred())
				Expect(result.Rows).To(Equal(expectedRows))
			},
			Entry("config queries", v1.View{
				Spec: v1.ViewSpec{
					Columns: []pkgView.ViewColumnDef{
						{
							Name:       "name",
							Type:       pkgView.ColumnTypeString,
							PrimaryKey: true,
						},
						{
							Name: "status",
							Type: pkgView.ColumnTypeString,
						},
					},
					Queries: map[string]pkgView.Query{
						"nodes": {
							Configs: &types.ResourceSelector{
								Types:       []string{"Kubernetes::Node"},
								TagSelector: "account=flanksource",
							},
						},
					},
					Mapping: map[string]types.CelExpression{
						"name":   "row.name",
						"status": "row.status",
					},
				},
			}, []pkgView.Row{
				{"node-a", "healthy"},
				{"node-b", "healthy"},
			}),
			Entry("changes queries", v1.View{
				Spec: v1.ViewSpec{
					Columns: []pkgView.ViewColumnDef{
						{
							Name:       "name",
							Type:       pkgView.ColumnTypeString,
							PrimaryKey: true,
						},
						{
							Name: "status",
							Type: pkgView.ColumnTypeString,
						},
					},
					Queries: map[string]pkgView.Query{
						"items": {
							Changes: &types.ResourceSelector{
								Search: "change_type=CREATE",
							},
						},
					},
					Mapping: map[string]types.CelExpression{
						"name":   "row.name",
						"status": "row.type",
					},
				},
			}, []pkgView.Row{
				{"Production EKS", "EKS::Cluster"},
				{"node-a", "Kubernetes::Node"},
			}),
			Entry("helm release changes queries", v1.View{
				Spec: v1.ViewSpec{
					Columns: []pkgView.ViewColumnDef{
						{
							Name:       "chart",
							Type:       pkgView.ColumnTypeString,
							PrimaryKey: true,
						},
						{
							Name:       "version",
							Type:       pkgView.ColumnTypeString,
							PrimaryKey: true,
						},
						{
							Name: "source",
							Type: pkgView.ColumnTypeString,
						},
					},
					Queries: map[string]pkgView.Query{
						"releases": {
							Changes: &types.ResourceSelector{
								Types:  []string{"Helm::Release"},
								Search: "change_type=UPDATE",
							},
						},
					},
					Mapping: map[string]types.CelExpression{
						"chart":   "row.name",
						"version": "row.summary.split(' to ')[1]",
						"source":  "row.source",
					},
				},
			}, []pkgView.Row{
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
			columns  []pkgView.ViewColumnDef
			mapping  map[string]types.CelExpression
			expected pkgView.Row
		}

		DescribeTable("applyMapping test cases",
			func(tc applyMappingTestCase) {
				row, err := applyMapping(tc.data, tc.columns, tc.mapping)
				Expect(err).ToNot(HaveOccurred())
				Expect(pkgView.Row(row)).To(Equal(tc.expected))
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
				columns: []pkgView.ViewColumnDef{
					{
						Name: "pod_name",
						Type: pkgView.ColumnTypeString,
					},
					{
						Name: "pod_status",
						Type: pkgView.ColumnTypeString,
					},
				},
				expected: pkgView.Row{"test-pod", "Running"},
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
				columns: []pkgView.ViewColumnDef{
					{
						Name: "duration",
						Type: pkgView.ColumnTypeDuration,
					},
				},
				expected: pkgView.Row{1 * time.Minute},
			}),
		)
	})
})
