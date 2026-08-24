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
		batchRows, batchTotal, err := pgGetAccessBatch[T](ctx, client, table, batch)
		if err != nil {
			return nil, 0, err
		}
		rows = append(rows, batchRows...)
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

func pgGetAccessBatch[T any](ctx context.Context, client *Client, table string, params url.Values) ([]T, int, error) {
	requestedLimit, _ := strconv.Atoi(params.Get("limit"))
	rows := make([]T, 0)
	total := -1
	for {
		pageParams := cloneValues(params)
		if len(rows) > 0 {
			pageParams.Set("offset", strconv.Itoa(len(rows)))
			pageParams.Set("limit", strconv.Itoa(requestedLimit-len(rows)))
		}
		var page []T
		pageTotal, err := client.pgGet(ctx, table, pageParams, &page)
		if err != nil {
			return nil, 0, err
		}
		if total >= 0 && pageTotal >= 0 && pageTotal != total {
			return nil, 0, fmt.Errorf("%s total changed from %d to %d while paging", table, total, pageTotal)
		}
		if total < 0 {
			total = pageTotal
		}
		rows = append(rows, page...)
		if requestedLimit == 0 || total < 0 || len(rows) >= min(requestedLimit, total) {
			return rows, total, nil
		}
		if len(page) == 0 {
			return nil, 0, fmt.Errorf("%s returned no rows at offset %d before reported total %d", table, len(rows), total)
		}
	}
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
