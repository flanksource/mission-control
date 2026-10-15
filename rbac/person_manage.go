package rbac

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"strconv"

	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/labstack/echo/v4"

	"github.com/flanksource/incident-commander/rbac/adapter"
)

// PersonManage authorizes an account operation: inviting, disabling, re-enabling, changing or deleting a person
// (specs/authorization/roles.md, Section 2.3).
func PersonManage() MiddlewareFunc {
	return Authorization(policy.ObjectPeople, adapter.ActionPersonManage)
}

// accountTables are the tables /db writes to as account operations, so writing them takes person:manage.
// Reading them is unchanged. Inserting person_analytics doesn't, as its rows are recorded as the signed-in person's.
var accountTables = map[string]struct{}{
	"people":           {},
	"identities":       {},
	"person_analytics": {},
}

// recordAnalyticsAsCaller sets the person of every row a /db insert into person_analytics records to the signed-in
// person, whichever person the request names, so any signed-in person records their own analytics and only theirs.
// The insert must be JSON, so the rows can be rewritten.
func recordAnalyticsAsCaller(c echo.Context, ctx context.Context) error {
	user := ctx.User()
	if user == nil {
		return dutyAPI.Errorf(dutyAPI.EUNAUTHORIZED, "not signed in")
	}
	if mediaType, _, err := mime.ParseMediaType(c.Request().Header.Get(echo.HeaderContentType)); err != nil || mediaType != echo.MIMEApplicationJSON {
		return dutyAPI.Errorf(dutyAPI.EINVALID, "person_analytics can only be recorded with a JSON body")
	}

	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return dutyAPI.Errorf(dutyAPI.EINVALID, "failed to read request body: %v", err)
	}

	var out any
	var rows []map[string]any
	if err := json.Unmarshal(body, &rows); err == nil {
		out = rows
	} else {
		var row map[string]any
		if err := json.Unmarshal(body, &row); err != nil {
			return dutyAPI.Errorf(dutyAPI.EINVALID, "invalid request body: %v", err)
		}
		rows, out = []map[string]any{row}, row
	}
	for _, row := range rows {
		row["person_id"] = user.ID.String()
	}

	if body, err = json.Marshal(out); err != nil {
		return ctx.Oops().Wrap(err)
	}
	c.Request().Body = io.NopCloser(bytes.NewReader(body))
	c.Request().ContentLength = int64(len(body))
	c.Request().Header.Set(echo.HeaderContentLength, strconv.Itoa(len(body)))
	return nil
}
