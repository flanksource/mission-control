package rbac

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"net/http"

	dutyAPI "github.com/flanksource/duty/api"
	"github.com/labstack/echo/v4"
)

// refusePermissionDeny refuses a write to /db/permissions that sets deny, as an unknown field: Permissions only allow.
// Inserts and updates must be JSON, so it can read which fields they set.
func refusePermissionDeny(c echo.Context) error {
	switch c.Request().Method {
	case http.MethodPost, http.MethodPatch, http.MethodPut:
	default:
		return nil
	}

	if mediaType, _, err := mime.ParseMediaType(c.Request().Header.Get(echo.HeaderContentType)); err != nil || mediaType != echo.MIMEApplicationJSON {
		return dutyAPI.Errorf(dutyAPI.EINVALID, "permissions can only be written with a JSON body")
	}

	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return dutyAPI.Errorf(dutyAPI.EINVALID, "failed to read request body: %v", err)
	}
	c.Request().Body = io.NopCloser(bytes.NewReader(body))

	var rows []map[string]json.RawMessage
	if err := json.Unmarshal(body, &rows); err != nil {
		var row map[string]json.RawMessage
		if err := json.Unmarshal(body, &row); err != nil {
			return dutyAPI.Errorf(dutyAPI.EINVALID, "invalid request body: %v", err)
		}
		rows = []map[string]json.RawMessage{row}
	}

	for _, row := range rows {
		if _, ok := row["deny"]; ok {
			return dutyAPI.Errorf(dutyAPI.EINVALID, `invalid request body: unknown field "deny": permissions only allow`)
		}
	}
	return nil
}
