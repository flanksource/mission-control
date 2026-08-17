package clientcmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/flanksource/clicky"
	clickyapi "github.com/flanksource/clicky/api"
	"github.com/flanksource/incident-commander/clientapi"
)

type playbookSQLResult clientapi.PlaybookSQLResult

func (r playbookSQLResult) String() string   { return r.table().String() }
func (r playbookSQLResult) ANSI() string     { return "\n" + r.table().ANSI() }
func (r playbookSQLResult) HTML() string     { return r.table().HTML() }
func (r playbookSQLResult) Markdown() string { return "\n" + r.table().Markdown() }

func (r playbookSQLResult) table() clickyapi.TextTable {
	headers := make([]clickyapi.Textable, len(r.Columns))
	for i, column := range r.Columns {
		headers[i] = clicky.Text(column, "font-bold")
	}

	rows := make([]clickyapi.TableRow, len(r.Rows))
	for i, row := range r.Rows {
		tableRow := make(clickyapi.TableRow)
		for _, column := range r.Columns {
			value := "NULL"
			if raw, ok := row[column]; ok && raw != nil {
				value = fmt.Sprint(raw)
			}
			tableRow[column] = clickyapi.TypedValue{Textable: clicky.Text(value, "")}
		}
		rows[i] = tableRow
	}

	return clickyapi.TextTable{Headers: headers, Rows: rows, FieldNames: r.Columns}
}

type playbookExecResult clientapi.PlaybookExecResult

func (r playbookExecResult) String() string   { return r.plain(false) }
func (r playbookExecResult) ANSI() string     { return r.plain(true) }
func (r playbookExecResult) HTML() string     { return "<pre>" + r.plain(false) + "</pre>" }
func (r playbookExecResult) Markdown() string { return "```\n" + r.plain(false) + "\n```" }

func (r playbookExecResult) plain(colors bool) string {
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

type playbookHTTPResult clientapi.PlaybookHTTPResult

func (r playbookHTTPResult) String() string   { return r.plain(false) }
func (r playbookHTTPResult) ANSI() string     { return r.plain(true) }
func (r playbookHTTPResult) HTML() string     { return "<pre>" + r.plain(false) + "</pre>" }
func (r playbookHTTPResult) Markdown() string { return "```\n" + r.plain(false) + "\n```" }

func (r playbookHTTPResult) plain(colors bool) string {
	var b strings.Builder
	status := fmt.Sprintf("Status: %d", r.StatusCode)
	if colors {
		style := "text-green-600"
		if r.StatusCode >= 400 {
			style = "text-red-600"
		}
		b.WriteString(clicky.Text(status, "font-bold "+style).ANSI())
	} else {
		b.WriteString(status)
	}
	b.WriteString("\n")

	keys := make([]string, 0, len(r.Headers))
	for key := range r.Headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintf(&b, "  %s: %s\n", key, r.Headers[key])
	}
	if r.Content != "" {
		b.WriteString("\n")
		b.WriteString(r.Content)
	}
	return strings.TrimRight(b.String(), "\n")
}
