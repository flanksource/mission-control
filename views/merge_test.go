package views

import (
	"testing"

	"github.com/flanksource/duty/types"
	. "github.com/onsi/gomega"

	v1 "github.com/flanksource/incident-commander/api/v1"
)

func TestMerge(t *testing.T) {
	tests := []struct {
		name         string
		queryResults []QueryResult
		mergeSpec    v1.ViewMergeSpec
		expected     []QueryResultRow
	}{
		{
			name: "basic left join with matching records",
			queryResults: []QueryResult{
				{
					Name: "users",
					Rows: []QueryResultRow{
						{"id": "1", "name": "Alice"},
						{"id": "2", "name": "Bob"},
					},
				},
				{
					Name: "orders",
					Rows: []QueryResultRow{
						{"id": "1", "amount": 100},
						{"id": "3", "amount": 200},
					},
				},
			},
			mergeSpec: v1.ViewMergeSpec{
				Strategy: v1.ViewMergeStrategyLeft,
				Order:    []string{"users", "orders"},
				JoinOn: map[string]types.CelExpression{
					"users":  types.CelExpression("row.id"),
					"orders": types.CelExpression("row.id"),
				},
			},
			expected: []QueryResultRow{
				{
					"users":  QueryResultRow{"id": "1", "name": "Alice"},
					"orders": QueryResultRow{"id": "1", "amount": 100},
				},
				{
					"users":  QueryResultRow{"id": "2", "name": "Bob"},
					"orders": nil,
				},
			},
		},
		{
			name: "left join with no matching records",
			queryResults: []QueryResult{
				{
					Name: "users",
					Rows: []QueryResultRow{
						{"id": "1", "name": "Alice"},
					},
				},
				{
					Name: "orders",
					Rows: []QueryResultRow{
						{"id": "2", "amount": 100},
					},
				},
			},
			mergeSpec: v1.ViewMergeSpec{
				Strategy: v1.ViewMergeStrategyLeft,
				Order:    []string{"users", "orders"},
				JoinOn: map[string]types.CelExpression{
					"users":  types.CelExpression("row.id"),
					"orders": types.CelExpression("row.id"),
				},
			},
			expected: []QueryResultRow{
				{
					"users":  QueryResultRow{"id": "1", "name": "Alice"},
					"orders": nil,
				},
			},
		},
		{
			name: "left join with multiple matching queries",
			queryResults: []QueryResult{
				{
					Name: "users",
					Rows: []QueryResultRow{
						{"id": "1", "name": "Alice"},
					},
				},
				{
					Name: "orders",
					Rows: []QueryResultRow{
						{"id": "1", "amount": 100},
					},
				},
				{
					Name: "addresses",
					Rows: []QueryResultRow{
						{"id": "1", "address": "123 Main St"},
					},
				},
			},
			mergeSpec: v1.ViewMergeSpec{
				Strategy: v1.ViewMergeStrategyLeft,
				Order:    []string{"users", "orders", "addresses"},
				JoinOn: map[string]types.CelExpression{
					"users":     types.CelExpression("row.id"),
					"orders":    types.CelExpression("row.id"),
					"addresses": types.CelExpression("row.id"),
				},
			},
			expected: []QueryResultRow{
				{
					"users":     QueryResultRow{"id": "1", "name": "Alice"},
					"orders":    QueryResultRow{"id": "1", "amount": 100},
					"addresses": QueryResultRow{"id": "1", "address": "123 Main St"},
				},
			},
		},
		{
			name: "left join with custom join expression",
			queryResults: []QueryResult{
				{
					Name: "pods",
					Rows: []QueryResultRow{
						{"name": "pod1", "namespace": "default"},
						{"name": "pod2", "namespace": "kube-system"},
					},
				},
				{
					Name: "services",
					Rows: []QueryResultRow{
						{"name": "pod1", "namespace": "default", "port": 80},
					},
				},
			},
			mergeSpec: v1.ViewMergeSpec{
				Strategy: v1.ViewMergeStrategyLeft,
				Order:    []string{"pods", "services"},
				JoinOn: map[string]types.CelExpression{
					"pods":     types.CelExpression("row.name + '-' + row.namespace"),
					"services": types.CelExpression("row.name + '-' + row.namespace"),
				},
			},
			expected: []QueryResultRow{
				{
					"pods":     QueryResultRow{"name": "pod1", "namespace": "default"},
					"services": QueryResultRow{"name": "pod1", "namespace": "default", "port": 80},
				},
				{
					"pods":     QueryResultRow{"name": "pod2", "namespace": "kube-system"},
					"services": nil,
				},
			},
		},
		{
			name: "left join with no match due to different join values",
			queryResults: []QueryResult{
				{
					Name: "base",
					Rows: []QueryResultRow{
						{"name": "app1", "namespace": "default"},
					},
				},
				{
					Name: "joined",
					Rows: []QueryResultRow{
						{"name": "app1", "namespace": "production"},
					},
				},
			},
			mergeSpec: v1.ViewMergeSpec{
				Strategy: v1.ViewMergeStrategyLeft,
				Order:    []string{"base", "joined"},
				JoinOn: map[string]types.CelExpression{
					"base":   types.CelExpression("row.name + '-' + row.namespace"),
					"joined": types.CelExpression("row.name + '-' + row.namespace"),
				},
			},
			expected: []QueryResultRow{
				{
					"base":   QueryResultRow{"name": "app1", "namespace": "default"},
					"joined": nil,
				},
			},
		},
		{
			name:         "empty queryResults returns nil",
			queryResults: []QueryResult{},
			mergeSpec:    v1.ViewMergeSpec{},
			expected:     nil,
		},
		{
			name: "single query returns all records",
			queryResults: []QueryResult{
				{
					Name: "users",
					Rows: []QueryResultRow{
						{"id": "1", "name": "Alice"},
						{"id": "2", "name": "Bob"},
					},
				},
			},
			expected: []QueryResultRow{
				{
					"users": QueryResultRow{"id": "1", "name": "Alice"},
				},
				{
					"users": QueryResultRow{"id": "2", "name": "Bob"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			RegisterTestingT(t)
			result, err := mergeResults(tt.queryResults, tt.mergeSpec)
			Expect(err).To(BeNil())
			Expect(result).To(Equal(tt.expected))
		})
	}
}
