package clientcli

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"html"
	"os"
	"reflect"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/flanksource/incident-commander/clientcli/api"
	"sigs.k8s.io/yaml"
)

type formatManager struct{}

var Formatter formatManager

func Text(content string, styles ...string) api.Text {
	return api.Text{Content: content, Style: strings.Join(styles, " ")}
}

func Collapsed(label string, content api.Textable, _ ...string) api.Collapsed {
	return api.Collapsed{Label: label, Content: content}
}

func Map(values map[string]string, _ ...string) api.Text {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+values[key])
	}
	return Text(strings.Join(parts, ", "))
}

func Format(data any, options ...FormatOptions) (string, error) {
	return Formatter.FormatWithOptions(mergeOptions(options...), data)
}

func MustPrint(data any, options ...FormatOptions) {
	output, err := Format(data, options...)
	if err != nil {
		panic(err)
	}
	fmt.Print(output)
	if output != "" && !strings.HasSuffix(output, "\n") {
		fmt.Println()
	}
}

func (formatManager) FormatWithOptions(options FormatOptions, data any) (string, error) {
	if options.Filter != "" {
		return "", fmt.Errorf("--filter is not available in the lightweight client")
	}
	if options.Tree {
		return "", fmt.Errorf("--tree is not available in the lightweight client")
	}
	format := legacyFormat(options)
	if format == "" {
		format = canonicalFormat(options.Format)
	}
	if format == "" {
		format = "pretty"
	}
	if options.Table {
		if format != "pretty" {
			return "", fmt.Errorf("--table cannot be combined with --%s", format)
		}
		table, ok := tableData(data)
		if !ok {
			return "", fmt.Errorf("--table requires a list of records")
		}
		return renderTable(table, "pretty"), nil
	}

	switch format {
	case "json":
		value, err := json.MarshalIndent(data, "", "  ")
		return string(value), err
	case "yaml":
		value, err := yaml.Marshal(data)
		return string(value), err
	case "csv":
		return formatCSV(data)
	case "markdown":
		return formatMarkdown(data)
	case "html":
		return formatHTML(data)
	case "slack":
		text, err := formatPretty(data, true)
		if err != nil {
			return "", err
		}
		value, err := json.MarshalIndent(map[string]any{"blocks": []any{map[string]any{"type": "section", "text": map[string]string{"type": "mrkdwn", "text": text}}}}, "", "  ")
		return string(value), err
	case "pdf":
		return "", fmt.Errorf("PDF output is not available in the lightweight client; use --html and convert it externally")
	case "pretty":
		return formatPretty(data, options.NoColor)
	default:
		return "", fmt.Errorf("unsupported output format %q", format)
	}
}

func (m formatManager) FormatToFile(options FormatOptions, data any) error {
	output, err := m.FormatWithOptions(options, data)
	if err != nil {
		return err
	}
	if options.Output == "" {
		return fmt.Errorf("output file is required")
	}
	return os.WriteFile(options.Output, []byte(output), 0o644)
}

func formatPretty(data any, noColor bool) (string, error) {
	if value, ok := data.(pretty); ok {
		text := value.Pretty()
		if noColor {
			return text.String(), nil
		}
		return text.ANSI(), nil
	}
	if text, ok := data.(api.Textable); ok {
		if noColor {
			return text.String(), nil
		}
		return text.ANSI(), nil
	}
	if table, ok := tableData(data); ok {
		return renderTable(table, "pretty"), nil
	}
	return prettyValue(reflect.ValueOf(data), 0, noColor), nil
}

type renderedTable struct {
	headers []string
	rows    [][]string
}

type columnRow interface {
	Columns() []api.ColumnDef
	Row() map[string]any
}

type prettyRow interface {
	PrettyRow(any) map[string]api.Text
}

type pretty interface {
	Pretty() api.Text
}

func tableData(data any) (renderedTable, bool) {
	value := reflect.ValueOf(data)
	if !value.IsValid() || value.Kind() != reflect.Slice {
		return renderedTable{}, false
	}
	if value.Len() == 0 {
		return emptyTable(value.Type().Elem())
	}

	first := value.Index(0)
	var item any
	if first.Kind() == reflect.Pointer && first.IsNil() {
		item = reflect.New(first.Type().Elem()).Interface()
	} else {
		item = first.Interface()
	}
	if row, ok := item.(columnRow); ok {
		columns := row.Columns()
		table := renderedTable{headers: make([]string, len(columns))}
		for i, column := range columns {
			table.headers[i] = column.DisplayLabel()
		}
		for i := 0; i < value.Len(); i++ {
			values := make([]string, len(columns))
			if item, ok := rowItem(value.Index(i)); ok {
				if typed, ok := item.(columnRow); ok {
					row := typed.Row()
					for j, column := range columns {
						values[j] = cellString(row[column.Name])
					}
				}
			}
			table.rows = append(table.rows, values)
		}
		return table, true
	}

	if row, ok := item.(prettyRow); ok {
		firstRow := row.PrettyRow(nil)
		keys := prettyRowKeys(firstRow)
		table := renderedTable{headers: make([]string, len(keys))}
		for i, key := range keys {
			table.headers[i] = title(key)
		}
		for i := 0; i < value.Len(); i++ {
			values := make([]string, len(keys))
			if item, ok := rowItem(value.Index(i)); ok {
				if typed, ok := item.(prettyRow); ok {
					row := typed.PrettyRow(nil)
					for j, key := range keys {
						values[j] = row[key].String()
					}
				}
			}
			table.rows = append(table.rows, values)
		}
		return table, true
	}

	typeOfItem := first.Type()
	if typeOfItem.Kind() == reflect.Pointer {
		typeOfItem = typeOfItem.Elem()
	}
	if typeOfItem.Kind() != reflect.Struct || typeOfItem == reflect.TypeOf(time.Time{}) {
		return renderedTable{}, false
	}
	fields := exportedFields(typeOfItem)
	table := renderedTable{headers: make([]string, len(fields))}
	for i, field := range fields {
		table.headers[i] = title(field.name)
	}
	for i := 0; i < value.Len(); i++ {
		row := value.Index(i)
		if row.Kind() == reflect.Pointer {
			if row.IsNil() {
				table.rows = append(table.rows, make([]string, len(fields)))
				continue
			}
			row = row.Elem()
		}
		values := make([]string, len(fields))
		for j, field := range fields {
			values[j] = cellString(row.Field(field.index).Interface())
		}
		table.rows = append(table.rows, values)
	}
	return table, true
}

func emptyTable(itemType reflect.Type) (renderedTable, bool) {
	var item any
	if itemType.Kind() == reflect.Pointer {
		item = reflect.New(itemType.Elem()).Interface()
	} else {
		item = reflect.Zero(itemType).Interface()
	}
	if row, ok := item.(columnRow); ok {
		columns := row.Columns()
		table := renderedTable{headers: make([]string, len(columns))}
		for i, column := range columns {
			table.headers[i] = column.DisplayLabel()
		}
		return table, true
	}
	if row, ok := item.(prettyRow); ok {
		values := row.PrettyRow(nil)
		keys := prettyRowKeys(values)
		table := renderedTable{headers: make([]string, len(keys))}
		for i, key := range keys {
			table.headers[i] = title(key)
		}
		return table, true
	}
	typeOfItem := itemType
	if typeOfItem.Kind() == reflect.Pointer {
		typeOfItem = typeOfItem.Elem()
	}
	if typeOfItem.Kind() != reflect.Struct || typeOfItem == reflect.TypeOf(time.Time{}) {
		return renderedTable{}, false
	}
	fields := exportedFields(typeOfItem)
	table := renderedTable{headers: make([]string, len(fields))}
	for i, field := range fields {
		table.headers[i] = title(field.name)
	}
	return table, true
}

func prettyRowKeys(row map[string]api.Text) []string {
	preferred := []string{"name", "type", "class", "health", "status", "cost", "age"}
	keys := make([]string, 0, len(row))
	for _, key := range preferred {
		if _, exists := row[key]; exists {
			keys = append(keys, key)
		}
	}
	return keys
}

func rowItem(value reflect.Value) (any, bool) {
	if value.Kind() == reflect.Pointer && value.IsNil() {
		return nil, false
	}
	return value.Interface(), true
}

type fieldInfo struct {
	index int
	name  string
}

func exportedFields(value reflect.Type) []fieldInfo {
	fields := make([]fieldInfo, 0, value.NumField())
	for i := 0; i < value.NumField(); i++ {
		field := value.Field(i)
		if field.PkgPath != "" || field.Anonymous {
			continue
		}
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name == "-" {
			continue
		}
		if name == "" {
			name = field.Name
		}
		fields = append(fields, fieldInfo{index: i, name: name})
	}
	return fields
}

func formatCSV(data any) (string, error) {
	table, ok := tableData(data)
	if !ok {
		return "", fmt.Errorf("CSV output requires a list of records")
	}
	var b strings.Builder
	w := csv.NewWriter(&b)
	if err := w.Write(table.headers); err != nil {
		return "", err
	}
	for _, row := range table.rows {
		if err := w.Write(row); err != nil {
			return "", err
		}
	}
	w.Flush()
	return strings.TrimSuffix(b.String(), "\n"), w.Error()
}

func formatMarkdown(data any) (string, error) {
	if value, ok := data.(pretty); ok {
		return value.Pretty().Markdown(), nil
	}
	if text, ok := data.(api.Textable); ok {
		return text.Markdown(), nil
	}
	table, ok := tableData(data)
	if !ok {
		return "```yaml\n" + prettyValue(reflect.ValueOf(data), 0, true) + "\n```", nil
	}
	return renderTable(table, "markdown"), nil
}

func formatHTML(data any) (string, error) {
	if value, ok := data.(pretty); ok {
		return htmlDocument(value.Pretty().HTML()), nil
	}
	if text, ok := data.(api.Textable); ok {
		return htmlDocument(text.HTML()), nil
	}
	table, ok := tableData(data)
	if !ok {
		plain := prettyValue(reflect.ValueOf(data), 0, true)
		return htmlDocument("<pre>" + html.EscapeString(plain) + "</pre>"), nil
	}
	return htmlDocument(renderTable(table, "html")), nil
}

func htmlDocument(body string) string {
	return "<!DOCTYPE html>\n<html><head><meta charset=\"utf-8\"><title>Faro output</title></head><body>" + body + "</body></html>"
}

func renderTable(table renderedTable, format string) string {
	switch format {
	case "markdown":
		var b strings.Builder
		b.WriteString("| " + strings.Join(table.headers, " | ") + " |\n|")
		for range table.headers {
			b.WriteString(" --- |")
		}
		for _, row := range table.rows {
			b.WriteString("\n| " + strings.Join(row, " | ") + " |")
		}
		return b.String()
	case "html":
		var b strings.Builder
		b.WriteString("<table><thead><tr>")
		for _, header := range table.headers {
			b.WriteString("<th>" + html.EscapeString(header) + "</th>")
		}
		b.WriteString("</tr></thead><tbody>")
		for _, row := range table.rows {
			b.WriteString("<tr>")
			for _, value := range row {
				b.WriteString("<td>" + html.EscapeString(value) + "</td>")
			}
			b.WriteString("</tr>")
		}
		b.WriteString("</tbody></table>")
		return b.String()
	default:
		rows := append([][]string{table.headers}, table.rows...)
		widths := make([]int, len(table.headers))
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
}

func prettyValue(value reflect.Value, indent int, noColor bool) string {
	if !value.IsValid() {
		return "null"
	}
	if value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return "null"
		}
		return prettyValue(value.Elem(), indent, noColor)
	}
	if value.CanInterface() {
		if text, ok := value.Interface().(api.Textable); ok {
			if noColor {
				return text.String()
			}
			return text.ANSI()
		}
		if timestamp, ok := value.Interface().(time.Time); ok {
			return timestamp.Format(time.RFC3339)
		}
	}
	pad := strings.Repeat(" ", indent)
	switch value.Kind() {
	case reflect.Struct:
		var b strings.Builder
		typeOfValue := value.Type()
		for i := 0; i < value.NumField(); i++ {
			field := typeOfValue.Field(i)
			if field.PkgPath != "" {
				continue
			}
			tag := strings.Split(field.Tag.Get("json"), ",")
			if tag[0] == "-" || len(tag) > 1 && tag[1] == "omitempty" && value.Field(i).IsZero() {
				continue
			}
			name := tag[0]
			if name == "" {
				name = field.Name
			}
			formatted := prettyValue(value.Field(i), indent+2, noColor)
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			if strings.Contains(formatted, "\n") || composite(value.Field(i)) {
				fmt.Fprintf(&b, "%s%s:\n%s%s", pad, title(name), strings.Repeat(" ", indent+2), strings.ReplaceAll(formatted, "\n", "\n"+strings.Repeat(" ", indent+2)))
			} else {
				fmt.Fprintf(&b, "%s%s: %s", pad, title(name), formatted)
			}
		}
		return b.String()
	case reflect.Map:
		keys := value.MapKeys()
		sort.Slice(keys, func(i, j int) bool { return fmt.Sprint(keys[i].Interface()) < fmt.Sprint(keys[j].Interface()) })
		var b strings.Builder
		for _, key := range keys {
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			formatted := prettyValue(value.MapIndex(key), indent+2, noColor)
			fmt.Fprintf(&b, "%s%s: %s", pad, title(fmt.Sprint(key.Interface())), formatted)
		}
		return b.String()
	case reflect.Slice, reflect.Array:
		var b strings.Builder
		for i := 0; i < value.Len(); i++ {
			if i > 0 {
				b.WriteByte('\n')
			}
			fmt.Fprintf(&b, "%s- %s", pad, prettyValue(value.Index(i), indent+2, noColor))
		}
		return b.String()
	case reflect.String:
		return value.String()
	case reflect.Bool:
		return fmt.Sprint(value.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return fmt.Sprint(value.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return fmt.Sprint(value.Uint())
	case reflect.Float32, reflect.Float64:
		return fmt.Sprint(value.Float())
	default:
		if value.CanInterface() {
			return fmt.Sprint(value.Interface())
		}
		return ""
	}
}

func composite(value reflect.Value) bool {
	if value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return false
		}
		value = value.Elem()
	}
	return value.Kind() == reflect.Struct || value.Kind() == reflect.Map || value.Kind() == reflect.Slice || value.Kind() == reflect.Array
}

func cellString(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(api.Textable); ok {
		return text.String()
	}
	reflection := reflect.ValueOf(value)
	if reflection.Kind() == reflect.Pointer {
		if reflection.IsNil() {
			return ""
		}
		return cellString(reflection.Elem().Interface())
	}
	if timestamp, ok := value.(time.Time); ok {
		return timestamp.Format(time.RFC3339)
	}
	if reflection.Kind() == reflect.Slice || reflection.Kind() == reflect.Array {
		parts := make([]string, reflection.Len())
		for i := range reflection.Len() {
			parts[i] = cellString(reflection.Index(i).Interface())
		}
		return strings.Join(parts, ", ")
	}
	return fmt.Sprint(value)
}

func title(value string) string {
	value = strings.ReplaceAll(value, "_", " ")
	var words []string
	start := 0
	for i, r := range value {
		if i > start && unicode.IsUpper(r) {
			words = append(words, value[start:i])
			start = i
		}
	}
	words = append(words, value[start:])
	for i, word := range words {
		word = strings.TrimSpace(word)
		if word == "" {
			continue
		}
		words[i] = strings.ToUpper(word[:1]) + strings.ToLower(word[1:])
	}
	return strings.Join(words, " ")
}
