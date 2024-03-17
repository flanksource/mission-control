package db

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/flanksource/duty/query"
	"github.com/labstack/echo/v4"
	"github.com/samber/lo"
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
func parseTimestampField(key, val string) (string, time.Time, error) {
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

	expr, err := datemath.Parse(val)
	if err != nil {
		return "", time.Time{}, err
	}

	return operator, expr.Time(), nil
}

func SearchQueryTransformMiddleware() func(echo.HandlerFunc) echo.HandlerFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			queryParam, err := transformQuery(c.QueryParams())
			if err != nil {
				return err
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
func transformQuery(queryParam url.Values) (url.Values, error) {
	for k, values := range queryParam {
		if !strings.HasSuffix(k, ".filter") || len(values) == 0 {
			continue
		}

		queryParam.Del(k)

		key := strings.TrimSuffix(k, ".filter")
		val := values[0] // Use the first one. We don't use multiple values.

		if operator, timestamp, err := parseTimestampField(key, val); err != nil {
			return nil, fmt.Errorf("invalid datemath expression (%q) for field (%s): %w", val, key, err)
		} else if !timestamp.IsZero() {
			queryParam.Add(key, fmt.Sprintf("%s.%s", operator, timestamp.Format(time.RFC3339)))
		} else {
			in, notIN, prefixes, suffixes := query.ParseFilteringQuery(val)
			if len(in) > 0 {
				queryParam.Add(key, fmt.Sprintf("in.(%s)", postgrestValues(in)))
			}

			if len(notIN) > 0 {
				queryParam.Add(key, fmt.Sprintf("not.in.(%s)", postgrestValues(notIN)))
			}

			for _, p := range prefixes {
				queryParam.Add(key, fmt.Sprintf("like.%s*", p))
			}

			for _, p := range suffixes {
				queryParam.Add(key, fmt.Sprintf("like.*%s", p))
			}
		}
	}

	return queryParam, nil
}

// postgrestValues returns ["a", "b", "c"] as `"a","b","c"`
func postgrestValues(val []any) string {
	return strings.Join(lo.Map(val, func(s any, i int) string {
		return fmt.Sprintf(`"%s"`, s)
	}), ",")
}
