package views

import (
	"fmt"

	"github.com/flanksource/clicky"
	clickyAPI "github.com/flanksource/clicky/api"
	"github.com/flanksource/duty/view"

	"github.com/flanksource/incident-commander/api"
)

func isInternalColumn(c view.ColumnDef) bool {
	return c.Type == "row_attributes" || c.Type == "grants"
}

type viewResultRow struct {
	columns []view.ColumnDef
	data    view.Row
}

func (r viewResultRow) Columns() []clickyAPI.ColumnDef {
	var cols []clickyAPI.ColumnDef
	for _, c := range r.columns {
		if c.Hidden || isInternalColumn(c) {
			continue
		}
		cols = append(cols, clickyAPI.ColumnDef{
			Name: c.Name,
			Type: string(c.Type),
		})
	}
	return cols
}

func (r viewResultRow) Row() map[string]any {
	out := make(map[string]any, len(r.columns))
	for i, c := range r.columns {
		if c.Hidden || isInternalColumn(c) {
			continue
		}
		if i < len(r.data) {
			out[c.Name] = r.data[i]
		}
	}
	return out
}

func buildTable(result *api.ViewResult) clickyAPI.TextTable {
	if len(result.Columns) == 0 || len(result.Rows) == 0 {
		return clickyAPI.TextTable{}
	}
	rows := make([]clickyAPI.TableProvider, len(result.Rows))
	for i, row := range result.Rows {
		rows[i] = viewResultRow{columns: result.Columns, data: row}
	}
	return clickyAPI.NewTableFrom(rows)
}

func Render(result *api.ViewResult) clickyAPI.TextList {
	var out clickyAPI.TextList

	out = append(out, clickyAPI.Text{
		Content: result.Title,
		Style:   "text-xl font-semibold",
	})

	if len(result.Variables) > 0 {
		var varItems []clickyAPI.Textable
		for _, v := range result.Variables {
			varItems = append(varItems, clickyAPI.Text{
				Content: fmt.Sprintf("%s = %s", v.Key, v.Default),
			})
		}
		out = append(out, clickyAPI.Text{
			Content:  "Variables",
			Style:    "font-semibold",
			Children: varItems,
		})
	}

	out = append(out, buildTable(result))
	return out
}

func RenderMulti(multi *api.MultiViewResult) clickyAPI.TextList {
	var out clickyAPI.TextList
	for i := range multi.Views {
		out = append(out, Render(&multi.Views[i])...)
	}
	return out
}

func RenderMultiHTML(multi *api.MultiViewResult) ([]byte, error) {
	s, err := clicky.Format(RenderMulti(multi), clicky.FormatOptions{Format: "html"})
	if err != nil {
		return nil, err
	}
	return []byte(s), nil
}

func RenderMultiPDF(multi *api.MultiViewResult) ([]byte, error) {
	s, err := clicky.Format(RenderMulti(multi), clicky.FormatOptions{Format: "pdf"})
	if err != nil {
		return nil, err
	}
	return []byte(s), nil
}

func RenderHTML(result *api.ViewResult) ([]byte, error) {
	s, err := clicky.Format(Render(result), clicky.FormatOptions{Format: "html"})
	if err != nil {
		return nil, err
	}
	return []byte(s), nil
}

func RenderPDF(result *api.ViewResult) ([]byte, error) {
	s, err := clicky.Format(Render(result), clicky.FormatOptions{Format: "pdf"})
	if err != nil {
		return nil, err
	}
	return []byte(s), nil
}
