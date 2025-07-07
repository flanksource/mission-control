package views

import (
	"fmt"
	"slices"

	v1 "github.com/flanksource/incident-commander/api/v1"
)

// mergeResults merges query results based on the specified strategy
func mergeResults(resultSet []QueryResult, merge v1.ViewMergeSpec) ([]QueryResultRow, error) {
	if len(resultSet) == 0 {
		return nil, nil
	}

	slices.SortFunc(resultSet, func(a, b QueryResult) int {
		return slices.Index(merge.Order, a.Name) - slices.Index(merge.Order, b.Name)
	})

	switch merge.Strategy {
	case v1.ViewMergeStrategyLeft:
		return joinLeft(resultSet, merge)
	case v1.ViewMergeStrategyUnion:
		return union(resultSet), nil
	default:
		return union(resultSet), nil
	}
}

func joinLeft(queryResults []QueryResult, merge v1.ViewMergeSpec) ([]QueryResultRow, error) {
	if len(queryResults) == 0 {
		return nil, nil
	}

	// Start with the first query as the base
	baseQuery := queryResults[0]

	inverted := []map[string]QueryResultRow{}
	for _, queryResult := range queryResults[1:] {
		inverted = append(inverted, invertQueryResultRows(queryResult, merge))
	}

	var mergedResults []QueryResultRow
	for _, baseRecord := range baseQuery.Rows {
		merged := QueryResultRow{
			baseQuery.Name: baseRecord,
		}

		baseJoinValue, err := getJoinValue(baseRecord, baseQuery.Name, merge)
		if err != nil {
			return nil, fmt.Errorf("failed to get join value for query %s: %w", baseQuery.Name, err)
		}

		for i, inverted := range inverted {
			if row, ok := inverted[baseJoinValue]; ok {
				merged[queryResults[i+1].Name] = row
			} else {
				merged[queryResults[i+1].Name] = nil
			}
		}

		mergedResults = append(mergedResults, merged)
	}

	return mergedResults, nil
}

func invertQueryResultRows(queryResult QueryResult, merge v1.ViewMergeSpec) map[string]QueryResultRow {
	pkValues := make(map[string]QueryResultRow)
	for _, row := range queryResult.Rows {
		joinValue, err := getJoinValue(row, queryResult.Name, merge)
		if err != nil {
			// Skip this row if we can't calculate the join value
			continue
		}
		pkValues[joinValue] = row
	}

	return pkValues
}

// getJoinValue calculates the join value for a row using the CEL expression
func getJoinValue(row QueryResultRow, queryName string, merge v1.ViewMergeSpec) (string, error) {
	if merge.JoinOn == nil {
		return "", fmt.Errorf("merge spec or joinOn not provided")
	}

	expr, ok := merge.JoinOn[queryName]
	if !ok {
		return "", fmt.Errorf("no join expression found for query %s", queryName)
	}

	result, err := expr.Eval(map[string]any{
		"row": row,
	})
	if err != nil {
		return "", fmt.Errorf("failed to evaluate join expression for query %s: %w", queryName, err)
	}

	return fmt.Sprintf("%v", result), nil
}

func union(queryResults []QueryResult) []QueryResultRow {
	var mergedResults []QueryResultRow
	for _, queryResult := range queryResults {
		for _, row := range queryResult.Rows {
			mergedResults = append(mergedResults, QueryResultRow{
				queryResult.Name: row,
			})
		}
	}

	return mergedResults
}
