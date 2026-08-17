package api

import (
	"fmt"
	"html"
	"strings"
)

type ColumnDef struct {
	Name  string
	Label string
}

type ColumnBuilder struct {
	column ColumnDef
}

func Column(name string) *ColumnBuilder {
	return &ColumnBuilder{column: ColumnDef{Name: name}}
}

func (b *ColumnBuilder) Label(label string) *ColumnBuilder {
	b.column.Label = label
	return b
}

func (b *ColumnBuilder) Build() ColumnDef {
	return b.column
}

func (c ColumnDef) DisplayLabel() string {
	if c.Label != "" {
		return c.Label
	}
	return title(c.Name)
}

type KeyValuePair struct {
	Key   string
	Value any
}

type DescriptionList struct {
	Items []KeyValuePair
}

func (d DescriptionList) String() string {
	var b strings.Builder
	for i, item := range d.Items {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "%s: %s", item.Key, plainValue(item.Value))
	}
	return b.String()
}

func (d DescriptionList) ANSI() string {
	var b strings.Builder
	for i, item := range d.Items {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(ansiText(item.Key+":", "font-bold"))
		b.WriteByte(' ')
		if value, ok := item.Value.(Textable); ok {
			b.WriteString(value.ANSI())
		} else {
			b.WriteString(fmt.Sprint(item.Value))
		}
	}
	return b.String()
}

func (d DescriptionList) HTML() string {
	var b strings.Builder
	b.WriteString("<dl>")
	for _, item := range d.Items {
		b.WriteString("<dt>" + html.EscapeString(item.Key) + "</dt><dd>")
		if value, ok := item.Value.(Textable); ok {
			b.WriteString(value.HTML())
		} else {
			b.WriteString(html.EscapeString(fmt.Sprint(item.Value)))
		}
		b.WriteString("</dd>")
	}
	b.WriteString("</dl>")
	return b.String()
}

func (d DescriptionList) Markdown() string {
	var b strings.Builder
	for _, item := range d.Items {
		fmt.Fprintf(&b, "- **%s:** %s\n", item.Key, plainValue(item.Value))
	}
	return strings.TrimSuffix(b.String(), "\n")
}

type TypedValue struct {
	Textable Textable
}

type TableRow map[string]TypedValue

type TextTable struct {
	Headers    []Textable
	Rows       []TableRow
	FieldNames []string
}

func (t TextTable) String() string {
	rows := make([][]string, 0, len(t.Rows)+1)
	headers := make([]string, len(t.Headers))
	for i, header := range t.Headers {
		headers[i] = header.String()
	}
	rows = append(rows, headers)
	for _, row := range t.Rows {
		values := make([]string, len(t.FieldNames))
		for i, field := range t.FieldNames {
			if value, ok := row[field]; ok && value.Textable != nil {
				values[i] = value.Textable.String()
			}
		}
		rows = append(rows, values)
	}
	return renderPlainTable(rows)
}

func (t TextTable) ANSI() string { return t.String() }

func (t TextTable) HTML() string {
	var b strings.Builder
	b.WriteString("<table><thead><tr>")
	for _, header := range t.Headers {
		b.WriteString("<th>" + header.HTML() + "</th>")
	}
	b.WriteString("</tr></thead><tbody>")
	for _, row := range t.Rows {
		b.WriteString("<tr>")
		for _, field := range t.FieldNames {
			b.WriteString("<td>")
			if value, ok := row[field]; ok && value.Textable != nil {
				b.WriteString(value.Textable.HTML())
			}
			b.WriteString("</td>")
		}
		b.WriteString("</tr>")
	}
	b.WriteString("</tbody></table>")
	return b.String()
}

func (t TextTable) Markdown() string {
	var b strings.Builder
	b.WriteString("| ")
	for i, header := range t.Headers {
		if i > 0 {
			b.WriteString(" | ")
		}
		b.WriteString(header.Markdown())
	}
	b.WriteString(" |\n|")
	for range t.Headers {
		b.WriteString(" --- |")
	}
	b.WriteByte('\n')
	for _, row := range t.Rows {
		b.WriteString("| ")
		for i, field := range t.FieldNames {
			if i > 0 {
				b.WriteString(" | ")
			}
			if value, ok := row[field]; ok && value.Textable != nil {
				b.WriteString(value.Textable.Markdown())
			}
		}
		b.WriteString(" |\n")
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func plainValue(value any) string {
	if textable, ok := value.(Textable); ok {
		return textable.String()
	}
	return fmt.Sprint(value)
}

func title(value string) string {
	value = strings.ReplaceAll(value, "_", " ")
	var b strings.Builder
	for i, word := range strings.Fields(value) {
		if i > 0 {
			b.WriteByte(' ')
		}
		if len(word) > 0 {
			b.WriteString(strings.ToUpper(word[:1]))
			b.WriteString(strings.ToLower(word[1:]))
		}
	}
	return b.String()
}

func renderPlainTable(rows [][]string) string {
	if len(rows) == 0 {
		return ""
	}
	widths := make([]int, len(rows[0]))
	for _, row := range rows {
		for i, value := range row {
			if len(value) > widths[i] {
				widths[i] = len(value)
			}
		}
	}
	var b strings.Builder
	for rowIndex, row := range rows {
		for i, value := range row {
			if i > 0 {
				b.WriteString("  ")
			}
			b.WriteString(value)
			if i < len(row)-1 {
				b.WriteString(strings.Repeat(" ", widths[i]-len(value)))
			}
		}
		if rowIndex < len(rows)-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}
