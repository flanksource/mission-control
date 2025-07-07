package views

import (
	"slices"

	v1 "github.com/flanksource/incident-commander/api/v1"
)

// mergeResults merges query results based on the specified strategy
func mergeResults(resultSet []QueryResult, order []string, strategy v1.ViewMergeStrategy) []QueryResultRow {
	if len(resultSet) == 0 {
		return nil
	}

	slices.SortFunc(resultSet, func(a, b QueryResult) int {
		return slices.Index(order, a.Name) - slices.Index(order, b.Name)
	})

	switch strategy {
	case v1.ViewMergeStrategyLeft:
		return joinLeft(resultSet)
	case v1.ViewMergeStrategyUnion:
		return union(resultSet)
	default:
		return nil
	}
}

func joinLeft(queryResults []QueryResult) []QueryResultRow {
	if len(queryResults) == 0 {
		return nil
	}

	// Start with the first query as the base
	baseQuery := queryResults[0]

	inverted := []map[string]QueryResultRow{}
	for _, queryResult := range queryResults[1:] {
		inverted = append(inverted, invertQueryResultRows(queryResult))
	}

	var mergedResults []QueryResultRow
	for _, baseRecord := range baseQuery.Rows {
		merged := QueryResultRow{
			baseQuery.Name: baseRecord,
		}

		for i, inverted := range inverted {
			if row, ok := inverted[baseRecord.PK(baseQuery.PrimaryKey)]; ok {
				merged[queryResults[i+1].Name] = row
			} else {
				merged[queryResults[i+1].Name] = nil
			}
		}

		mergedResults = append(mergedResults, merged)
	}

	return mergedResults
}

func invertQueryResultRows(queryResult QueryResult) map[string]QueryResultRow {
	pkValues := make(map[string]QueryResultRow)
	for _, row := range queryResult.Rows {
		pkValues[row.PK(queryResult.PrimaryKey)] = row
	}

	return pkValues
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
