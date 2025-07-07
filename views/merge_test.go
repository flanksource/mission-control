package views

import (
	"testing"

	. "github.com/onsi/gomega"
)

func TestJoinLeft(t *testing.T) {
	tests := []struct {
		name         string
		queryResults []QueryResult
		expected     []QueryResultRow
	}{
		{
			name: "basic left join with matching records",
			queryResults: []QueryResult{
				{
					PrimaryKey: []string{"id"},
					Name:       "users",
					Rows: []QueryResultRow{
						{"id": "1", "name": "Alice"},
						{"id": "2", "name": "Bob"},
					},
				},
				{
					PrimaryKey: []string{"id"},
					Name:       "orders",
					Rows: []QueryResultRow{
						{"id": "1", "amount": 100},
						{"id": "3", "amount": 200},
					},
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
					PrimaryKey: []string{"id"},
					Name:       "users",
					Rows: []QueryResultRow{
						{"id": "1", "name": "Alice"},
					},
				},
				{
					PrimaryKey: []string{"id"},
					Name:       "orders",
					Rows: []QueryResultRow{
						{"id": "2", "amount": 100},
					},
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
					PrimaryKey: []string{"id"},
					Name:       "users",
					Rows: []QueryResultRow{
						{"id": "1", "name": "Alice"},
					},
				},
				{
					PrimaryKey: []string{"id"},
					Name:       "orders",
					Rows: []QueryResultRow{
						{"id": "1", "amount": 100},
					},
				},
				{
					PrimaryKey: []string{"id"},
					Name:       "addresses",
					Rows: []QueryResultRow{
						{"id": "1", "address": "123 Main St"},
					},
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
			name: "left join with custom primary key",
			queryResults: []QueryResult{
				{
					PrimaryKey: []string{"name", "namespace"},
					Name:       "pods",
					Rows: []QueryResultRow{
						{"name": "pod1", "namespace": "default"},
						{"name": "pod2", "namespace": "kube-system"},
					},
				},
				{
					PrimaryKey: []string{"name", "namespace"},
					Name:       "services",
					Rows: []QueryResultRow{
						{"name": "pod1", "namespace": "default", "port": 80},
					},
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
			name: "left join with partial primary key match (no match)",
			queryResults: []QueryResult{
				{
					PrimaryKey: []string{"name", "namespace"},
					Name:       "base",
					Rows: []QueryResultRow{
						{"name": "app1", "namespace": "default"},
					},
				},
				{
					PrimaryKey: []string{"name", "namespace"},
					Name:       "joined",
					Rows: []QueryResultRow{
						{"name": "app1", "namespace": "production"},
					},
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
			expected:     nil,
		},
		{
			name: "single query returns all records",
			queryResults: []QueryResult{
				{
					PrimaryKey: []string{"id"},
					Name:       "users",
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
		{
			name: "fallback to common fields when no primary key specified",
			queryResults: []QueryResult{
				{
					PrimaryKey: []string{}, // Empty primary key should fallback to common fields
					Name:       "base",
					Rows: []QueryResultRow{
						{"id": "1", "name": "Alice"},
					},
				},
				{
					PrimaryKey: []string{},
					Name:       "joined",
					Rows: []QueryResultRow{
						{"id": "1", "name": "Alice", "extra": "data"},
					},
				},
			},
			expected: []QueryResultRow{
				{
					"base":   QueryResultRow{"id": "1", "name": "Alice"},
					"joined": QueryResultRow{"id": "1", "name": "Alice", "extra": "data"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			RegisterTestingT(t)
			result := joinLeft(tt.queryResults)
			Expect(result).To(Equal(tt.expected))
		})
	}
}
