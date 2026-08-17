package clientcli

import (
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type AllFlags struct {
	FormatOptions
	LevelCount int
	JsonLogs   bool
}

var Flags = &AllFlags{}

func BindAllFlags(flags *pflag.FlagSet, filters ...string) *AllFlags {
	flags.CountVarP(&Flags.LevelCount, "loglevel", "v", "Increase logging level")
	flags.BoolVar(&Flags.JsonLogs, "json-logs", false, "Print logs in JSON format to stderr")
	flags.StringVar(&Flags.Format, "format", "", "Output format: pretty, json, yaml, csv, html, or markdown; format=file sinks may be comma-separated")
	flags.StringVar(&Flags.Filter, "filter", "", "Filter expression (not available in the lightweight client)")
	flags.BoolVar(&Flags.NoColor, "no-color", false, "Disable colored output")
	flags.BoolVar(&Flags.DumpSchema, "dump-schema", false, "Dump the output schema")
	flags.BoolVar(&Flags.JSON, "json", false, "Output in JSON format")
	flags.BoolVar(&Flags.YAML, "yaml", false, "Output in YAML format")
	flags.BoolVar(&Flags.CSV, "csv", false, "Output in CSV format")
	flags.BoolVar(&Flags.Markdown, "markdown", false, "Output in Markdown format")
	flags.BoolVar(&Flags.Pretty, "pretty", false, "Output in pretty format")
	flags.BoolVar(&Flags.HTML, "html", false, "Output in HTML format")
	flags.BoolVar(&Flags.PDF, "pdf", false, "Output in PDF format")
	flags.BoolVar(&Flags.Tree, "tree", false, "Display in tree structure")
	flags.BoolVar(&Flags.Table, "table", false, "Display in table structure")
	return Flags
}

func SetGroupedUsage(_ *cobra.Command) {}
