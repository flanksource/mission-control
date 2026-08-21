package api

import (
	"fmt"
	"sort"
	"strings"

	"github.com/flanksource/artifacts"
	"github.com/flanksource/clicky"
	clickyapi "github.com/flanksource/clicky/api"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/shell"
	"github.com/flanksource/incident-commander/clientapi"
)

// PlaybookListItem is the response struct for listing playbooks
// for a filter/selector.
type PlaybookListItem = clientapi.PlaybookListItem

// PlaybookSQLResult is the result of a SQL playbook action.
type PlaybookSQLResult struct {
	Query   string           `json:"query,omitempty"`
	Rows    []map[string]any `json:"rows,omitempty"`
	Count   int              `json:"count"`
	Columns []string         `json:"columns,omitempty"` // Used for maintaining order in UI
}

func (r PlaybookSQLResult) String() string   { return r.table().String() }
func (r PlaybookSQLResult) ANSI() string     { return "\n" + r.table().ANSI() }
func (r PlaybookSQLResult) HTML() string     { return r.table().HTML() }
func (r PlaybookSQLResult) Markdown() string { return "\n" + r.table().Markdown() }

func (r PlaybookSQLResult) table() clickyapi.TextTable {
	headers := make([]clickyapi.Textable, len(r.Columns))
	for i, col := range r.Columns {
		headers[i] = clicky.Text(col, "font-bold")
	}

	rows := make([]clickyapi.TableRow, len(r.Rows))
	for i, row := range r.Rows {
		tr := make(clickyapi.TableRow)
		for _, col := range r.Columns {
			val := "NULL"
			if v, exists := row[col]; exists && v != nil {
				val = fmt.Sprint(v)
			}
			tr[col] = clickyapi.TypedValue{Textable: clicky.Text(val, "")}
		}
		rows[i] = tr
	}

	return clickyapi.TextTable{
		Headers:    headers,
		Rows:       rows,
		FieldNames: r.Columns,
	}
}

// PlaybookExecResult is the result of an exec playbook action.
type PlaybookExecResult shell.ExecDetails

func (r *PlaybookExecResult) GetArtifacts() []artifacts.Artifact {
	if r == nil {
		return nil
	}
	return r.Artifacts
}

func (r *PlaybookExecResult) GetStatus() models.PlaybookActionStatus {
	if r.ExitCode != 0 {
		return models.PlaybookActionStatusFailed
	}
	return models.PlaybookActionStatusCompleted
}

func (r PlaybookExecResult) String() string   { return r.plain(false) }
func (r PlaybookExecResult) ANSI() string     { return r.plain(true) }
func (r PlaybookExecResult) HTML() string     { return "<pre>" + r.plain(false) + "</pre>" }
func (r PlaybookExecResult) Markdown() string { return "```\n" + r.plain(false) + "\n```" }

func (r PlaybookExecResult) plain(colors bool) string {
	var b strings.Builder

	if r.Stdout != "" {
		if colors {
			b.WriteString(clicky.Text("Stdout:", "font-bold text-green-600").ANSI())
		} else {
			b.WriteString("Stdout:")
		}
		b.WriteString("\n")
		b.WriteString(strings.TrimSpace(r.Stdout))
		b.WriteString("\n")
	}

	if r.Stderr != "" {
		if colors {
			b.WriteString(clicky.Text("Stderr:", "font-bold text-red-600").ANSI())
		} else {
			b.WriteString("Stderr:")
		}
		b.WriteString("\n")
		b.WriteString(strings.TrimSpace(r.Stderr))
		b.WriteString("\n")
	}

	if r.ExitCode != 0 {
		if colors {
			b.WriteString(clicky.Text(fmt.Sprintf("Exit Code: %d", r.ExitCode), "text-red-600").ANSI())
		} else {
			fmt.Fprintf(&b, "Exit Code: %d", r.ExitCode)
		}
		b.WriteString("\n")
	}

	return strings.TrimRight(b.String(), "\n")
}

// PlaybookHTTPResult is the result of an HTTP playbook action.
type PlaybookHTTPResult struct {
	Content    string            `json:"content"`
	Headers    map[string]string `json:"headers"`
	StatusCode int               `json:"code"`
}

func (r PlaybookHTTPResult) String() string   { return r.plain(false) }
func (r PlaybookHTTPResult) ANSI() string     { return r.plain(true) }
func (r PlaybookHTTPResult) HTML() string     { return "<pre>" + r.plain(false) + "</pre>" }
func (r PlaybookHTTPResult) Markdown() string { return "```\n" + r.plain(false) + "\n```" }

func (r PlaybookHTTPResult) plain(colors bool) string {
	var b strings.Builder

	statusLabel := fmt.Sprintf("Status: %d", r.StatusCode)
	if colors {
		style := "text-green-600"
		if r.StatusCode >= 400 {
			style = "text-red-600"
		}
		b.WriteString(clicky.Text(statusLabel, "font-bold "+style).ANSI())
	} else {
		b.WriteString(statusLabel)
	}
	b.WriteString("\n")

	if len(r.Headers) > 0 {
		keys := make([]string, 0, len(r.Headers))
		for k := range r.Headers {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(&b, "  %s: %s\n", k, r.Headers[k])
		}
	}

	if r.Content != "" {
		b.WriteString("\n")
		b.WriteString(r.Content)
	}

	return strings.TrimRight(b.String(), "\n")
}
