package db

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/query"
	"github.com/labstack/echo/v4"
	"github.com/timberio/go-datemath"
)

var dateFields = map[string]struct{}{
	"acknowledged":         {},
	"check_time":           {},
	"closed":               {},
	"created_at":           {},
	"deleted_at":           {},
	"end_time":             {},
	"expires_at":           {},
	"first_observed":       {},
	"last_attempt":         {},
	"last_login":           {},
	"last_observed":        {},
	"last_received":        {},
	"last_runtime":         {},
	"last_scraped_time":    {},
	"last_seen":            {},
	"last_transition_time": {},
	"next_runtime":         {},
	"resolved":             {},
	"scheduled_time":       {},
	"silenced_at":          {},
	"start_time":           {},
	"time":                 {},
	"time_end":             {},
	"time_start":           {},
	"updated_at":           {},
}

// parseTimestampField returns the postgREST operator (eq, gt, lt)
// and the parsed datemath.
func parseTimestampField(now time.Time, key, val string) (string, time.Time, error) {
	_, ok := dateFields[key]
	if !ok {
		return "", time.Time{}, nil
	}

	operator := "lt" // default if no operator is supplied
	if strings.HasPrefix(val, "=") {
		operator = "eq"
		val = strings.TrimPrefix(val, "=")
	} else if strings.HasPrefix(val, ">") {
		operator = "gt"
		val = strings.TrimPrefix(val, ">")
	} else if strings.HasPrefix(val, "<") {
		operator = "lt"
		val = strings.TrimPrefix(val, "<")
	}

	parsedTime, err := datemath.ParseAndEvaluate(val, datemath.WithNow(now))
	if err != nil {
		return "", time.Time{}, err
	}

	return operator, parsedTime, nil
}

func SearchQueryTransformMiddleware() func(echo.HandlerFunc) echo.HandlerFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			queryParam, err := transformQuery(time.Now(), c.QueryParams())
			if err != nil {
				return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINVALID, "%s", err.Error()))
			}

			c.Request().URL.RawQuery = queryParam.Encode()

			// NOTE: Had to modify this explicitly otherwise
			// postgREST will receive the original URL.
			c.Request().RequestURI = c.Request().URL.String()

			return next(c)
		}
	}
}

// transformQuery transforms any search query to native postgREST query
func transformQuery(now time.Time, queryParam url.Values) (url.Values, error) {
	filterKeys := make([]string, 0)
	for key := range queryParam {
		if strings.HasSuffix(key, ".filter") {
			filterKeys = append(filterKeys, key)
		}
	}
	sort.Strings(filterKeys)

	logicalTerms := make([]string, 0)
	for _, filterKey := range filterKeys {
		values := queryParam[filterKey]
		queryParam.Del(filterKey)
		if len(values) == 0 {
			continue
		}

		field := strings.TrimSuffix(filterKey, ".filter")
		if _, isDate := dateFields[field]; isDate {
			if err := addTimestampFilters(now, field, values, queryParam); err != nil {
				return nil, err
			}
			continue
		}

		terms, err := postgrestMatchItemTerms(field, values)
		if err != nil {
			return nil, fmt.Errorf("invalid filter for field %s: %w", field, err)
		}
		logicalTerms = append(logicalTerms, terms...)
	}
	if len(logicalTerms) > 0 {
		queryParam.Add("and", "("+strings.Join(logicalTerms, ",")+")")
	}

	return queryParam, nil
}

func addTimestampFilters(now time.Time, field string, values []string, queryParam url.Values) error {
	for _, value := range values {
		operator, timestamp, err := parseTimestampField(now, field, value)
		if err != nil {
			return fmt.Errorf("invalid datemath expression (%q) for field (%s): %w", value, field, err)
		}
		queryParam.Add(field, fmt.Sprintf("%s.%s", operator, timestamp.Format(time.RFC3339)))
	}
	return nil
}

func postgrestMatchItemTerms(field string, values []string) ([]string, error) {
	positive := make([]string, 0)
	negative := make([]string, 0)
	for _, value := range values {
		filter, err := query.ParseFilteringQuery(value, true)
		if err != nil {
			return nil, err
		}
		positive = append(positive, postgrestPatterns(field, "ilike", filter.In, filter.Prefix, filter.Suffix, filter.Glob)...)
		negative = append(negative, postgrestPatterns(field, "not.ilike", filter.Not.In, filter.Not.Prefix, filter.Not.Suffix, filter.Not.Glob)...)
	}

	terms := make([]string, 0, len(negative)+1)
	if len(positive) > 0 {
		terms = append(terms, "or("+strings.Join(positive, ",")+")")
	}
	return append(terms, negative...), nil
}

func postgrestPatterns(field, operator string, exact []any, prefix, suffix, glob []string) []string {
	patterns := make([]string, 0, len(exact)+len(prefix)+len(suffix)+len(glob))
	for _, value := range exact {
		patterns = append(patterns, fmt.Sprintf("%s.%s.%v", field, operator, value))
	}
	for _, value := range prefix {
		patterns = append(patterns, fmt.Sprintf("%s.%s.%s*", field, operator, value))
	}
	for _, value := range suffix {
		patterns = append(patterns, fmt.Sprintf("%s.%s.*%s", field, operator, value))
	}
	for _, value := range glob {
		patterns = append(patterns, fmt.Sprintf("%s.%s.*%s*", field, operator, value))
	}
	return patterns
}
