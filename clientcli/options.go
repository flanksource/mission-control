package clientcli

import (
	"fmt"
	"strings"
)

type FormatSink struct {
	Format string
	File   string
}

type FormatOptions struct {
	Format     string
	NoColor    bool
	Output     string
	Verbose    bool
	DumpSchema bool
	Filter     string
	JSON       bool
	YAML       bool
	CSV        bool
	Markdown   bool
	Pretty     bool
	HTML       bool
	PDF        bool
	Slack      bool
	Tree       bool
	Table      bool
	Page       int
	Limit      int
	Sinks      []FormatSink
}

func (o *FormatOptions) ParseFormatSpec() error {
	if len(o.Sinks) > 0 {
		return nil
	}
	if strings.TrimSpace(o.Format) == "" {
		if format := legacyFormat(*o); format != "" {
			o.Sinks = []FormatSink{{Format: format}}
		}
		return nil
	}

	stdout := 0
	for _, raw := range strings.Split(o.Format, ",") {
		part := strings.TrimSpace(raw)
		if part == "" {
			continue
		}
		name, file, hasFile := strings.Cut(part, "=")
		name = canonicalFormat(strings.TrimSpace(name))
		file = strings.TrimSpace(file)
		if !supportedFormat(name) {
			return fmt.Errorf("invalid --format entry %q: unknown format %q", part, name)
		}
		if hasFile && file == "" {
			return fmt.Errorf("invalid --format entry %q: empty file path after '='", part)
		}
		if !hasFile {
			stdout++
			if stdout > 1 {
				return fmt.Errorf("invalid --format %q: more than one stdout format specified", o.Format)
			}
		}
		o.Sinks = append(o.Sinks, FormatSink{Format: name, File: file})
	}
	return nil
}

func supportedFormat(format string) bool {
	switch canonicalFormat(format) {
	case "pretty", "json", "yaml", "csv", "markdown", "html", "pdf", "slack":
		return true
	default:
		return false
	}
}

func canonicalFormat(format string) string {
	switch strings.ToLower(format) {
	case "yml":
		return "yaml"
	case "md":
		return "markdown"
	default:
		return strings.ToLower(format)
	}
}

func legacyFormat(options FormatOptions) string {
	switch {
	case options.JSON:
		return "json"
	case options.YAML:
		return "yaml"
	case options.CSV:
		return "csv"
	case options.HTML:
		return "html"
	case options.Markdown:
		return "markdown"
	case options.PDF:
		return "pdf"
	case options.Slack:
		return "slack"
	case options.Pretty:
		return "pretty"
	default:
		return ""
	}
}

func mergeOptions(options ...FormatOptions) FormatOptions {
	var merged FormatOptions
	for _, option := range options {
		if option.Format != "" {
			merged.Format = option.Format
		}
		if option.Output != "" {
			merged.Output = option.Output
		}
		if option.Filter != "" {
			merged.Filter = option.Filter
		}
		merged.NoColor = merged.NoColor || option.NoColor
		merged.Verbose = merged.Verbose || option.Verbose
		merged.DumpSchema = merged.DumpSchema || option.DumpSchema
		merged.JSON = merged.JSON || option.JSON
		merged.YAML = merged.YAML || option.YAML
		merged.CSV = merged.CSV || option.CSV
		merged.Markdown = merged.Markdown || option.Markdown
		merged.Pretty = merged.Pretty || option.Pretty
		merged.HTML = merged.HTML || option.HTML
		merged.PDF = merged.PDF || option.PDF
		merged.Slack = merged.Slack || option.Slack
		merged.Tree = merged.Tree || option.Tree
		merged.Table = merged.Table || option.Table
		if option.Page > 0 {
			merged.Page = option.Page
		}
		if option.Limit > 0 {
			merged.Limit = option.Limit
		}
		if len(option.Sinks) > 0 {
			merged.Sinks = append(merged.Sinks, option.Sinks...)
		}
	}
	return merged
}
