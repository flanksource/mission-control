package clientcmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/flanksource/clicky"
)

func printClicky(w io.Writer, data any, defaultFormat string) error {
	opts := clicky.Flags.FormatOptions
	if err := opts.ParseFormatSpec(); err != nil {
		return err
	}

	if len(opts.Sinks) == 0 {
		return writeClickyOutput(w, data, clicky.FormatOptions{Format: defaultFormat}, opts)
	}

	for _, sink := range opts.Sinks {
		sinkOpts := opts
		sinkOpts.Sinks = nil
		sinkOpts.Format = sink.Format
		sinkOpts.JSON, sinkOpts.YAML, sinkOpts.CSV = false, false, false
		sinkOpts.HTML, sinkOpts.Markdown, sinkOpts.Pretty = false, false, false
		sinkOpts.PDF, sinkOpts.Slack = false, false
		if sink.File == "" {
			if err := writeClickyOutput(w, data, sinkOpts); err != nil {
				return err
			}
			continue
		}
		sinkOpts.Output = sink.File
		if err := clicky.Formatter.FormatToFile(sinkOpts, data); err != nil {
			return err
		}
	}
	return nil
}

func writeClickyOutput(w io.Writer, data any, opts ...clicky.FormatOptions) error {
	out, err := clicky.Format(data, opts...)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprint(w, out); err != nil {
		return err
	}
	if !strings.HasSuffix(out, "\n") {
		_, err = fmt.Fprintln(w)
	}
	return err
}
