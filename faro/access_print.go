package main

import (
	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
)

// flatExportFormats render an api.Pretty value as its pretty *text* rather than
// building a table from it (formatters/parser.go:177, csv_formatter.go:44,
// markdown_formatter.go:22 and html_formatter.go:206). A summary type that groups
// or annotates its rows would therefore export as the terminal rendering instead
// of a machine-readable table. pretty, json, yaml, slack and tree all represent
// the grouping faithfully and are deliberately absent.
var flatExportFormats = map[string]bool{
	"csv":         true,
	"markdown":    true,
	"html":        true,
	"html-static": true,
	"html-react":  true,
	"pdf":         true,
	"excel":       true,
}

// wantsFlatRows reports whether the selected output format needs the flat table
// rows instead of the grouped summary value. opts is a copy, so parsing the raw
// --format spec here cannot mutate the shared flag state.
func wantsFlatRows(opts clicky.FormatOptions) bool {
	if err := opts.ParseFormatSpec(); err != nil {
		return false
	}
	for _, sink := range opts.Sinks {
		if flatExportFormats[sink.Format] {
			return true
		}
	}
	return false
}

// printAccessResult prints summary for the formats that can represent its
// grouping (pretty, json, yaml) and the flat rows for the export formats, which
// cannot. Both carry the same data; only the shape differs.
func printAccessResult[T api.TableProvider](summary any, rows []T) {
	if wantsFlatRows(clicky.Flags.FormatOptions) {
		clicky.MustPrint(rows, clicky.Flags.FormatOptions)
		return
	}
	clicky.MustPrint(summary, clicky.Flags.FormatOptions)
}
