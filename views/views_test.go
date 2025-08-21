package views

import (
	"testing"
	"time"

	"github.com/flanksource/duty/connection"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/dataquery"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	pkgView "github.com/flanksource/duty/view"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"

	v1 "github.com/flanksource/incident-commander/api/v1"
)

func TestApplyMapping(t *testing.T) {
	type applyMappingTestCase struct {
		name     string
		data     map[string]any
		columns  []pkgView.ColumnDef
		mapping  map[string]types.CelExpression
		expected pkgView.Row
	}

	testCases := []applyMappingTestCase{
		{
			name: "should apply CEL expressions to data",
			data: map[string]any{
				"name":   "test-pod",
				"status": "Running",
				"ready":  true,
			},
			mapping: map[string]types.CelExpression{
				"pod_name":   "row.name",
				"pod_status": "row.status",
			},
			columns: []pkgView.ColumnDef{
				{
					Name: "pod_name",
					Type: pkgView.ColumnTypeString,
				},
				{
					Name: "pod_status",
					Type: pkgView.ColumnTypeString,
				},
			},
			expected: pkgView.Row{"test-pod", "Running", nil},
		},
		{
			name: "should handle empty mapping",
			data: map[string]any{
				"name": "test",
			},
			mapping:  map[string]types.CelExpression{},
			expected: pkgView.Row{nil},
		},
		{
			name: "helper columns",
			columns: []pkgView.ColumnDef{
				{
					Name: "name",
					Type: pkgView.ColumnTypeString,
				},
				{
					Name: "url",
					Type: pkgView.ColumnTypeString,
					For:  lo.ToPtr("name"),
				},
			},
			data: map[string]any{
				"name": "test",
			},
			mapping: map[string]types.CelExpression{
				"name": `row.name`,
				"url":  `"https://example.com/" + row.name`,
			},
			expected: pkgView.Row{"test", "https://example.com/test", nil},
		},
		{
			name: "no explicit mapping",
			columns: []pkgView.ColumnDef{
				{
					Name: "name",
					Type: pkgView.ColumnTypeString,
				},
				{
					Name: "url",
					Type: pkgView.ColumnTypeString,
					For:  lo.ToPtr("name"),
				},
			},
			data: map[string]any{
				"name": "test",
				"url":  "https://example.com/test",
			},
			mapping:  nil,
			expected: pkgView.Row{"test", "https://example.com/test", nil},
		},
		{
			name: "should handle durations",
			data: map[string]any{
				"duration": "does not matter. the value is hardcoded in the mapping.",
			},
			mapping: map[string]types.CelExpression{
				"duration": "duration('1m')",
			},
			columns: []pkgView.ColumnDef{
				{
					Name: "duration",
					Type: pkgView.ColumnTypeDuration,
				},
			},
			expected: pkgView.Row{1 * time.Minute, nil},
		},
	}

	for _, tc := range testCases {
		ctx := context.New()
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			row, err := applyMapping(ctx, tc.data, tc.columns, tc.mapping)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(pkgView.Row(row)).To(Equal(tc.expected))
		})
	}
}

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
					Columns: []pkgView.ColumnDef{
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
					Queries: map[string]v1.ViewQueryWithColumnDefs{
						"nodes": {
							Query: pkgView.Query{
								Configs: &types.ResourceSelector{
									Types:       []string{"Kubernetes::Node"},
									TagSelector: "account=flanksource",
								},
							},
						},
					},
					Mapping: map[string]types.CelExpression{
						"name":   "row.name",
						"status": "row.status",
					},
				},
			}, []pkgView.Row{
				{"node-a", "healthy", nil},
				{"node-b", "healthy", nil},
			}),
			Entry("changes queries", v1.View{
				Spec: v1.ViewSpec{
					Columns: []pkgView.ColumnDef{
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
					Queries: map[string]v1.ViewQueryWithColumnDefs{
						"items": {
							Query: pkgView.Query{
								Changes: &types.ResourceSelector{
									Search: "change_type=CREATE",
								},
							},
						},
					},
					Mapping: map[string]types.CelExpression{
						"name":   "row.name",
						"status": "row.type",
					},
				},
			}, []pkgView.Row{
				{"Production EKS", "EKS::Cluster", nil},
				{"node-a", "Kubernetes::Node", nil},
			}),
			XEntry("helm release changes queries", v1.View{
				Spec: v1.ViewSpec{
					Columns: []pkgView.ColumnDef{
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
					Queries: map[string]v1.ViewQueryWithColumnDefs{
						"releases": {
							Query: pkgView.Query{
								Changes: &types.ResourceSelector{
									Types:  []string{"Helm::Release"},
									Search: "change_type=UPDATE",
								},
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
				{"nginx-ingress", "4.8.0", "Flux", nil},
				{"nginx-ingress", "4.7.2", "Flux", nil},
				{"nginx-ingress", "4.7.1", "Flux", nil},
				{"redis", "18.1.5", "Flux", nil},
				{"redis", "18.1.3", "Flux", nil},
				{"redis", "18.1.0", "Flux", nil},
			}),
			XEntry("prometheus query with empty results", v1.View{
				Spec: v1.ViewSpec{
					Columns: []pkgView.ColumnDef{
						{
							Name:       "pod",
							Type:       pkgView.ColumnTypeString,
							PrimaryKey: true,
						},
						{
							Name: "memory_usage",
							Type: pkgView.ColumnTypeNumber,
						},
					},
					Queries: map[string]v1.ViewQueryWithColumnDefs{
						"metrics": {
							Columns: map[string]models.ColumnType{
								"pod":   models.ColumnTypeString,
								"value": models.ColumnTypeDecimal,
							},
							Query: pkgView.Query{
								Query: dataquery.Query{
									Prometheus: &dataquery.PrometheusQuery{
										PrometheusConnection: connection.PrometheusConnection{
											HTTPConnection: connection.HTTPConnection{
												URL: "https://prometheus.demo.prometheus.io/",
											},
										},
										Query: `up{nonexistent_label="value"}`, // This should return no results
									},
								},
							},
						},
					},
					Mapping: map[string]types.CelExpression{
						"pod":          "row.pod || 'unknown'",
						"memory_usage": "row.value || 0",
					},
				},
			}, nil), // Empty results expected but should not error
		)

		It("should work without SQLite database when no panels or merge query", func() {
			view := v1.View{
				Spec: v1.ViewSpec{
					Columns: []pkgView.ColumnDef{
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
					Queries: map[string]v1.ViewQueryWithColumnDefs{
						"nodes": {
							Query: pkgView.Query{
								Configs: &types.ResourceSelector{
									Types:       []string{"Kubernetes::Node"},
									TagSelector: "account=flanksource",
								},
							},
						},
					},
					Mapping: map[string]types.CelExpression{
						"name":   "row.name",
						"status": "row.status",
					},
					// No panels and no merge query - should not create SQLite database
				},
			}

			result, err := Run(DefaultContext, &view)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())
			Expect(result.Rows).To(HaveLen(2))
		})
	})
})
