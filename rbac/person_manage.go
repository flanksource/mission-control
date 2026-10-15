package rbac

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"net/http"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/samber/lo"

	"github.com/flanksource/incident-commander/rbac/adapter"
)

// PersonManage authorizes an account operation: inviting, disabling, re-enabling, changing or deleting a person
// (specs/authorization/roles.md, Section 2.3).
func PersonManage() MiddlewareFunc {
	return Authorization(policy.ObjectPeople, adapter.ActionPersonManage)
}

// accountTables are the tables /db writes to as account operations, so writing them takes person:manage.
// Reading them is unchanged. A person may still record their own analytics (insertsOwnAnalytics).
var accountTables = map[string]struct{}{
	"people":           {},
	"identities":       {},
	"person_analytics": {},
}

// insertsOwnAnalytics reports whether the request inserts person_analytics rows only for the signed-in person,
// which any signed-in person may do. The insert must be JSON, so it can read whose rows they are.
func insertsOwnAnalytics(c echo.Context, ctx context.Context) bool {
	user := ctx.User()
	if user == nil || c.Request().Method != http.MethodPost {
		return false
	}
	if mediaType, _, err := mime.ParseMediaType(c.Request().Header.Get(echo.HeaderContentType)); err != nil || mediaType != echo.MIMEApplicationJSON {
		return false
	}

	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return false
	}
	c.Request().Body = io.NopCloser(bytes.NewReader(body))

	var rows []map[string]any
	if err := json.Unmarshal(body, &rows); err != nil {
		var row map[string]any
		if err := json.Unmarshal(body, &row); err != nil {
			return false
		}
		rows = []map[string]any{row}
	}

	return len(rows) > 0 && lo.EveryBy(rows, func(row map[string]any) bool {
		value, _ := row["person_id"].(string)
		id, err := uuid.Parse(value)
		return err == nil && id == user.ID
	})
}
