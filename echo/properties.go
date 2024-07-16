package echo

import (
	"net/http"

	"github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/incident-commander/db"
	echov4 "github.com/labstack/echo/v4"
)

func Properties(c echov4.Context) error {
	ctx := c.Request().Context().(context.Context)

	dbProperties, err := db.GetProperties(ctx)
	if err != nil {
		return api.WriteError(c, err)
	}

	var seen = make(map[string]bool)

	var output = make([]map[string]string, 0)

	for k, v := range context.Local {
		output = append(output, map[string]string{
			"name":        k,
			"value":       v,
			"source":      "local",
			"type":        "",
			"description": "",
		})
		seen[k] = true
	}

	for _, p := range dbProperties {
		if _, ok := seen[p.Name]; ok {
			continue
		}

		output = append(output, map[string]string{
			"name":        p.Name,
			"value":       p.Value,
			"source":      "db",
			"type":        "",
			"description": "",
		})
	}

	return c.JSON(http.StatusOK, output)
}
