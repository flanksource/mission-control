package echo

import (
	"net/http"

	"github.com/flanksource/clicky"
	clickyfmt "github.com/flanksource/clicky/formatters"
	dutyApi "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	echov4 "github.com/labstack/echo/v4"
)

// GetResource handles GET /resources/:id and returns a single catalog item.
//
// With Accept: application/json+clicky it renders as a clicky TreeNode / KeyValue
// view that the embedded UI drops into OperationEntityPage. Plain JSON callers
// get the models.ConfigItem as-is.
func GetResource(c echov4.Context) error {
	ctx := c.Request().Context().(context.Context)
	id := c.Param("id")
	if id == "" {
		return dutyApi.WriteError(c, dutyApi.Errorf(dutyApi.EINVALID, "id is required"))
	}

	item, err := query.ConfigItemFromCache(ctx, id)
	if err != nil {
		return dutyApi.WriteError(c, err)
	}

	if wantsClicky(c.Request().Header.Get("Accept")) {
		body, err := renderResourceClicky(item)
		if err != nil {
			return dutyApi.WriteError(c, err)
		}
		return c.Blob(http.StatusOK, "application/json+clicky", []byte(body))
	}

	return c.JSON(http.StatusOK, item)
}

func renderResourceClicky(item models.ConfigItem) (string, error) {
	body := item.Pretty()

	// Link back to the catalog list filtered to this config_type so the user
	// can navigate detail → list of siblings without leaving the app.
	if item.Type != nil {
		related := clicky.LinkCommand("searchResources").
			WithFlag("config_type", *item.Type).
			Append("All "+*item.Type, "text-sky-700 underline")
		body = body.NewLine().Append(related)
	}

	manager := clickyfmt.NewFormatManager()
	return manager.FormatWithOptions(clickyfmt.FormatOptions{Format: "clicky-json"}, body)
}
