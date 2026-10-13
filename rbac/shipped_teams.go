package rbac

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strings"

	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/flanksource/incident-commander/rbac/adapter"
)

// guardShippedTeams refuses a write through /db/teams that could change or delete a team Mission Control ships,
// and returns the error to respond with. Writes that change or delete teams must select them by id, so the request
// says which teams it touches, and inserts must be JSON, so the guard can read which teams they name.
// Their members are written through /db/team_members, which isn't guarded.
func guardShippedTeams(c echo.Context, ctx context.Context) error {
	var ids []uuid.UUID
	var names []string
	switch c.Request().Method {
	case http.MethodPost:
		// An upsert can overwrite an existing team, by its id or its name
		if mediaType, _, err := mime.ParseMediaType(c.Request().Header.Get(echo.HeaderContentType)); err != nil || mediaType != echo.MIMEApplicationJSON {
			return dutyAPI.Errorf(dutyAPI.EINVALID, "teams can only be created with a JSON body")
		}
		body, err := io.ReadAll(c.Request().Body)
		if err != nil {
			return dutyAPI.Errorf(dutyAPI.EINVALID, "failed to read request body: %v", err)
		}
		c.Request().Body = io.NopCloser(bytes.NewReader(body))
		if ids, names, err = teamsInBody(body); err != nil {
			return dutyAPI.Errorf(dutyAPI.EINVALID, "invalid request body: %v", err)
		}

	case http.MethodPatch, http.MethodPut, http.MethodDelete:
		var ok bool
		if ids, ok = teamIDsInFilter(c.Request().URL.Query()["id"]); !ok {
			return dutyAPI.Errorf(dutyAPI.EINVALID, "teams can only be changed or deleted by id, e.g. ?id=eq.<id> or ?id=in.(<id>,<id>)")
		}
	}

	if len(ids) == 0 && len(names) == 0 {
		return nil
	}

	var shipped []string
	if err := ctx.DB().Table("teams").Select("name").
		Where("source = ? AND deleted_at IS NULL", adapter.SourceShipped).
		Where("id IN ? OR name IN ?", append(ids, uuid.Nil), append(names, "")).
		Find(&shipped).Error; err != nil {
		return ctx.Oops().Wrap(err)
	} else if len(shipped) > 0 {
		return dutyAPI.Errorf(dutyAPI.ECONFLICT, "team %s is shipped with Mission Control and can't be changed or deleted; its members can", shipped[0])
	}
	return nil
}

// teamIDsInFilter returns the ids a PostgREST id filter selects: id=eq.<id> or id=in.(<id>,...).
// Any other filter, none, or a value that isn't a UUID isn't ok. Ids are compared as UUIDs, as Postgres does,
// so their spelling, e.g. upper case, doesn't matter.
func teamIDsInFilter(filters []string) ([]uuid.UUID, bool) {
	if len(filters) == 0 {
		return nil, false
	}

	var values []string
	for _, filter := range filters {
		if id, found := strings.CutPrefix(filter, "eq."); found {
			values = append(values, id)
		} else if list, found := strings.CutPrefix(filter, "in.("); found && strings.HasSuffix(list, ")") {
			for _, id := range strings.Split(strings.TrimSuffix(list, ")"), ",") {
				values = append(values, strings.Trim(strings.TrimSpace(id), `"`))
			}
		} else {
			return nil, false
		}
	}

	ids := make([]uuid.UUID, 0, len(values))
	for _, value := range values {
		id, err := uuid.Parse(value)
		if err != nil {
			return nil, false
		}
		ids = append(ids, id)
	}
	return ids, true
}

// teamsInBody returns the ids and names of the teams in a PostgREST JSON insert: one object or a list of them.
func teamsInBody(body []byte) (ids []uuid.UUID, names []string, err error) {
	var rows []map[string]any
	if err := json.Unmarshal(body, &rows); err != nil {
		var row map[string]any
		if err := json.Unmarshal(body, &row); err != nil {
			return nil, nil, err
		}
		rows = []map[string]any{row}
	}

	for _, row := range rows {
		if value, ok := row["id"].(string); ok {
			id, err := uuid.Parse(value)
			if err != nil {
				return nil, nil, err
			}
			ids = append(ids, id)
		}
		if name, ok := row["name"].(string); ok {
			names = append(names, name)
		}
	}
	return ids, names, nil
}
