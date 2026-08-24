package sdk

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

const accessQueryMaxLength = 6000

var accessBatchFilterKeys = []string{"config_id", "external_user_id", "external_group_id"}

func pgGetAccess[T any](ctx context.Context, client *Client, table string, params url.Values, compare func(T, T) int) ([]T, int, error) {
	batches, err := batchAccessParams(params)
	if err != nil {
		return nil, 0, err
	}

	rows := make([]T, 0)
	total := 0
	totalKnown := true
	for _, batch := range batches {
		var page []T
		batchTotal, err := client.pgGet(ctx, table, batch, &page)
		if err != nil {
			return nil, 0, err
		}
		rows = append(rows, page...)
		if batchTotal < 0 {
			totalKnown = false
		} else {
			total += batchTotal
		}
	}

	if compare != nil {
		sort.SliceStable(rows, func(i, j int) bool { return compare(rows[i], rows[j]) < 0 })
	}
	if limit, _ := strconv.Atoi(params.Get("limit")); limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	if !totalKnown {
		total = -1
	}
	return rows, total, nil
}

func batchAccessParams(params url.Values) ([]url.Values, error) {
	if len(params.Encode()) <= accessQueryMaxLength {
		return []url.Values{params}, nil
	}

	key, values := largestSplittableAccessFilter(params)
	if len(values) < 2 {
		return nil, fmt.Errorf("access query parameters are %d bytes and cannot be split below %d bytes", len(params.Encode()), accessQueryMaxLength)
	}

	middle := len(values) / 2
	left := cloneValues(params)
	left.Set(key, inList(values[:middle]))
	right := cloneValues(params)
	right.Set(key, inList(values[middle:]))

	leftBatches, err := batchAccessParams(left)
	if err != nil {
		return nil, err
	}
	rightBatches, err := batchAccessParams(right)
	if err != nil {
		return nil, err
	}
	return append(leftBatches, rightBatches...), nil
}

func largestSplittableAccessFilter(params url.Values) (string, []string) {
	var selectedKey string
	var selected []string
	for _, key := range accessBatchFilterKeys {
		values := parseInList(params.Get(key))
		if len(values) > len(selected) {
			selectedKey = key
			selected = values
		}
	}
	return selectedKey, selected
}

func parseInList(value string) []string {
	if !strings.HasPrefix(value, "in.(") || !strings.HasSuffix(value, ")") {
		return nil
	}
	return strings.Split(strings.TrimSuffix(strings.TrimPrefix(value, "in.("), ")"), ",")
}

func cloneValues(values url.Values) url.Values {
	clone := make(url.Values, len(values))
	for key, entries := range values {
		clone[key] = append([]string(nil), entries...)
	}
	return clone
}
