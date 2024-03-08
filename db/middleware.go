package db

import (
	"fmt"
	"strings"

	"github.com/flanksource/duty/query"
	"github.com/labstack/echo/v4"
	"github.com/samber/lo"
	"github.com/timberio/go-datemath"
)

var dateFields = []string{"created_at", "deleted_at", "updated_at", "last_scraped_time", "time"}

func isDateField(key string) bool {
	for _, df := range dateFields {
		if strings.HasPrefix(key, fmt.Sprintf("%s.", df)) {
			return true
		}
	}

	return false
}

func SearchQueryTransformMiddlware() func(echo.HandlerFunc) echo.HandlerFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			queryParam := c.QueryParams()

			for k, values := range queryParam {
				if !strings.HasSuffix(k, ".filter") || len(values) == 0 {
					continue
				}

				queryParam.Del(k)

				key := strings.TrimSuffix(k, ".filter")
				val := values[0] // Use the first one. We don't use multiple values.

				if isDateField(key) {
					expr, err := datemath.Parse(val)
					if err != nil {
						return fmt.Errorf("invalid datemath expression (%q) for field %q: %w", val, key, err)
					}

					c.Set(key, expr.Time())
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

			c.Request().URL.RawQuery = queryParam.Encode()

			// NOTE: Had to modify this explicitly otherwise
			// postgREST will receive the original URL.
			c.Request().RequestURI = c.Request().URL.String()
			return next(c)
		}
	}
}

// postgrestValues returns ["a", "b", "c"] as `"a","b","c"`
func postgrestValues(val []string) string {
	return strings.Join(lo.Map(val, func(s string, i int) string {
		return fmt.Sprintf(`"%s"`, s)
	}), ",")
}
