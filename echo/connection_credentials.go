package echo

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	dutyApi "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	echov4 "github.com/labstack/echo/v4"
	"github.com/samber/lo"

	"github.com/flanksource/incident-commander/db"
)

// Reading a connection never returns its credentials (specs/authorization/roles.md, Section 2.2).
// PostgREST returns the connections table as stored, so /db masks the credentials of every connection row it returns,
// at the top level or embedded in another table's rows. Masking is by field name, so a request that could rename
// a credential column, or reveal it through a filter or sort, is refused.

const connectionsTable = "connections"

var credentialColumns = []string{"username", "password", "certificate", "url", "properties"}

// postgrestParams are the query parameters that aren't filters on a column.
var postgrestParams = []string{"select", "order", "limit", "offset", "on_conflict", "columns", "or", "and", "not.or", "not.and"}

var logicalCredentialRef = regexp.MustCompile(`(^|[(,])\s*(not\.)?(` + strings.Join(credentialColumns, "|") + `)\s*(->|\.|::)`)

type connectionQuery struct {
	// table is the table the request is made on.
	table string

	// embeds are the response keys that hold embedded connection rows, by alias or by table name.
	embeds map[string]bool
}

func (q connectionQuery) involvesConnections() bool {
	return q.table == connectionsTable || len(q.embeds) > 0
}

func postgrestTable(path string) string {
	path = strings.TrimPrefix(path, "/db")
	return strings.Trim(path, "/")
}

// parseConnectionQuery finds where a /db request reads connections, and refuses it if it could reveal their credentials
// without returning them under their own column names.
func parseConnectionQuery(table string, query map[string][]string) (connectionQuery, error) {
	q := connectionQuery{table: table, embeds: map[string]bool{}}

	for _, sel := range query["select"] {
		if err := checkSelect(sel, table == connectionsTable, q.embeds); err != nil {
			return q, err
		}
	}

	for key, values := range query {
		prefix, param := "", key
		if i := strings.Index(key, "."); i > 0 && q.embeds[key[:i]] {
			prefix, param = key[:i], key[i+1:]
		}

		connectionScope := (prefix == "" && table == connectionsTable) || prefix != ""
		if !connectionScope {
			continue
		}

		for _, value := range values {
			switch {
			case param == "order":
				for _, term := range strings.Split(value, ",") {
					if isCredentialColumn(columnName(term)) {
						return q, credentialQueryError("sort on")
					}
				}
			case lo.Contains([]string{"or", "and", "not.or", "not.and"}, param):
				if logicalCredentialRef.MatchString(value) {
					return q, credentialQueryError("filter on")
				}
			case prefix != "" || !lo.Contains(postgrestParams, param):
				if isCredentialColumn(columnName(param)) {
					return q, credentialQueryError("filter on")
				}
			}
		}
	}

	return q, nil
}

func credentialQueryError(what string) error {
	return dutyApi.Errorf(dutyApi.EINVALID, "connection credentials are masked: requests can't %s, rename, cast or extract username, password, certificate, url or properties", what)
}

// columnName returns the column a select, filter or order term is on, e.g. password for "pw:password::text".
func columnName(term string) string {
	term = strings.TrimSpace(term)
	if alias, rest, ok := strings.Cut(term, ":"); ok && !strings.HasPrefix(rest, ":") && !strings.Contains(alias, "::") {
		term = rest
	}
	if i := strings.IndexAny(term, ".:-"); i >= 0 {
		term = term[:i]
	}
	return strings.TrimSpace(term)
}

func isCredentialColumn(column string) bool {
	return lo.Contains(credentialColumns, column)
}

// checkSelect walks a PostgREST select, recording where connections are embedded, and refusing credential columns of
// connections that are renamed, cast, extracted from or aggregated.
func checkSelect(sel string, onConnections bool, embeds map[string]bool) error {
	for _, item := range splitSelect(sel) {
		item = strings.TrimSpace(item)

		if open := strings.Index(item, "("); open >= 0 && strings.HasSuffix(item, ")") && !isAggregate(item, open) {
			head, inner := item[:open], item[open+1:len(item)-1]
			alias, relation, hasAlias := strings.Cut(head, ":")
			if !hasAlias {
				relation, alias = head, ""
			}
			spread := strings.HasPrefix(relation, "...")
			relation, _, _ = strings.Cut(strings.TrimPrefix(relation, "..."), "!")

			embedsConnections := relation == connectionsTable
			if embedsConnections {
				if spread {
					return dutyApi.Errorf(dutyApi.EINVALID, "connection credentials are masked: connections can't be spread into another table's rows")
				}
				embeds[lo.CoalesceOrEmpty(alias, relation)] = true
			}

			if err := checkSelect(inner, embedsConnections, embeds); err != nil {
				return err
			}
			continue
		}

		if !onConnections || item == "*" {
			continue
		}

		column := columnName(item)
		if isCredentialColumn(column) && item != column {
			return credentialQueryError("select,")
		}
	}

	return nil
}

func isAggregate(item string, open int) bool {
	return strings.HasSuffix(item[:open], ".count") || strings.HasSuffix(item[:open], ".sum") ||
		strings.HasSuffix(item[:open], ".avg") || strings.HasSuffix(item[:open], ".max") || strings.HasSuffix(item[:open], ".min")
}

// splitSelect splits a select on the commas that aren't inside an embed.
func splitSelect(sel string) []string {
	var items []string
	depth, start := 0, 0
	for i, r := range sel {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				items = append(items, sel[start:i])
				start = i + 1
			}
		}
	}
	return append(items, sel[start:])
}

// connectionCredentialsInterceptor guards /db requests that read or write connections. It refuses requests that could
// reveal credentials past the masking of responses, asks PostgREST for responses it can mask, and puts the stored
// credentials back into writes that send them masked, as read.
func connectionCredentialsInterceptor(next echov4.HandlerFunc) echov4.HandlerFunc {
	return func(c echov4.Context) error {
		q, err := parseConnectionQuery(postgrestTable(c.Request().URL.Path), c.Request().URL.Query())
		if err != nil {
			return dutyApi.WriteError(c, err)
		} else if !q.involvesConnections() {
			return next(c)
		}

		c.Request().Header.Del(echov4.HeaderAcceptEncoding)

		if q.table == connectionsTable && lo.Contains([]string{http.MethodPost, http.MethodPatch, http.MethodPut}, c.Request().Method) {
			if err := unmaskConnectionWrite(c); err != nil {
				return dutyApi.WriteError(c, err)
			}
		}

		return next(c)
	}
}

// unmaskConnectionWrite puts the stored credentials back into the connection rows of a write that sends them masked.
func unmaskConnectionWrite(c echov4.Context) error {
	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return dutyApi.Errorf(dutyApi.EINVALID, "failed to read request body: %v", err)
	}
	c.Request().Body = io.NopCloser(bytes.NewReader(body))
	if !bytes.Contains(body, []byte(db.MaskedValue)) {
		return nil
	}

	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil
	}

	rows := jsonObjects(payload)
	ctx := c.Request().Context().(context.Context)
	filterID := strings.TrimPrefix(c.QueryParam("id"), "eq.")
	for _, row := range rows {
		id := filterID
		if v, ok := row["id"].(string); ok && v != "" {
			id = v
		}
		if id == "" {
			continue
		}

		var stored models.Connection
		if tx := ctx.DB().Where("id = ?", id).Limit(1).Find(&stored); tx.Error != nil {
			return ctx.Oops().Wrap(tx.Error)
		} else if tx.RowsAffected == 0 {
			continue
		}
		db.UnmaskConnectionRow(row, stored)
	}

	body, err = json.Marshal(payload)
	if err != nil {
		return dutyApi.Errorf(dutyApi.EINVALID, "error marshaling request body: %v", err)
	}
	c.Request().Body = io.NopCloser(bytes.NewReader(body))
	c.Request().ContentLength = int64(len(body))
	c.Request().Header.Set(echov4.HeaderContentLength, strconv.Itoa(len(body)))
	return nil
}

// maskConnectionCredentials masks the credentials of the connection rows of a PostgREST response.
func maskConnectionCredentials(resp *http.Response) error {
	if resp.Request == nil || resp.Body == nil {
		return nil
	}

	q, err := parseConnectionQuery(postgrestTable(resp.Request.URL.Path), resp.Request.URL.Query())
	if err != nil || !q.involvesConnections() {
		return err
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()

	if len(bytes.TrimSpace(body)) > 0 && resp.StatusCode < 300 {
		if !strings.Contains(resp.Header.Get(echov4.HeaderContentType), "json") || resp.Header.Get("Content-Encoding") != "" {
			body = []byte(`{"error":"connections can only be read as uncompressed JSON, so that their credentials can be masked"}`)
			resp.StatusCode = http.StatusNotAcceptable
			resp.Header.Set(echov4.HeaderContentType, echov4.MIMEApplicationJSON)
			resp.Header.Del("Content-Encoding")
		} else {
			var payload any
			if err := json.Unmarshal(body, &payload); err != nil {
				return err
			}

			if q.table == connectionsTable {
				for _, row := range jsonObjects(payload) {
					db.MaskConnectionRow(row)
				}
			}
			maskEmbeddedConnections(payload, q.embeds)

			if body, err = json.Marshal(payload); err != nil {
				return err
			}
		}
	}

	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.ContentLength = int64(len(body))
	resp.Header.Set(echov4.HeaderContentLength, strconv.Itoa(len(body)))
	return nil
}

func maskEmbeddedConnections(value any, embeds map[string]bool) {
	switch v := value.(type) {
	case []any:
		for _, item := range v {
			maskEmbeddedConnections(item, embeds)
		}
	case map[string]any:
		for key, field := range v {
			if embeds[key] {
				for _, row := range jsonObjects(field) {
					db.MaskConnectionRow(row)
				}
			}
			maskEmbeddedConnections(field, embeds)
		}
	}
}

// jsonObjects returns the object, or the objects of the array, a JSON value holds.
func jsonObjects(value any) []map[string]any {
	switch v := value.(type) {
	case map[string]any:
		return []map[string]any{v}
	case []any:
		var objects []map[string]any
		for _, item := range v {
			if obj, ok := item.(map[string]any); ok {
				objects = append(objects, obj)
			}
		}
		return objects
	}
	return nil
}
