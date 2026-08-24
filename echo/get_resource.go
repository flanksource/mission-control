package echo

import (
	"errors"
	"net/http"

	"github.com/flanksource/clicky"
	clickyfmt "github.com/flanksource/clicky/formatters"
	dutyApi "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/google/uuid"
	echov4 "github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

// GetResource handles GET /resources/:id and returns a single catalog item.
//
// With Accept: application/json+clicky it renders as a clicky TreeNode / KeyValue
// view that the embedded UI drops into OperationEntityPage. Plain JSON callers
// get the models.ConfigItem as-is.
func GetResource(c echov4.Context) error {
	ctx := c.Request().Context().(context.Context)
	// Parsing here rather than letting the id reach the query is what separates a bad request from
	// a server fault: config_items.id is a uuid column, so a non-uuid path segment raises SQLSTATE
	// 22P02, which carries no domain code and would otherwise be reported as a 500.
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return dutyApi.WriteError(c, dutyApi.Errorf(dutyApi.EINVALID, "invalid config item id: %s", c.Param("id")))
	}

	item, err := query.ConfigItemFromCache(ctx, id.String())
	if err != nil {
		// ConfigItemFromCache reports a miss with a bare gorm.ErrRecordNotFound, which carries no
		// domain code, so WriteError would default it to EINTERNAL and answer a routine "no such
		// id" with a 500. The translation belongs here: gorm's error is the right contract for a
		// storage primitive, and this is the boundary where it becomes an HTTP status.
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return dutyApi.WriteError(c, dutyApi.Errorf(dutyApi.ENOTFOUND, "config item %s not found", id))
		}
		return dutyApi.WriteError(c, ctx.Oops().Wrapf(err, "get config item %s", id))
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
