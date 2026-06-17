package clientcmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/flanksource/clicky"
	"github.com/flanksource/commons/logger"
)

func shouldLogJSON() bool {
	return clicky.Flags.JsonLogs || logger.IsJsonLogs()
}

func Log(w io.Writer, data map[string]any) error {
	var out []byte
	if shouldLogJSON() {
		out, _ = json.Marshal(data)
	} else {
		out = formatKeyValues(data)
	}
	out = append(out, '\n')

	_, err := w.Write(out)
	return err
}

func formatKeyValues(data map[string]any) []byte {
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var out bytes.Buffer
	for i, key := range keys {
		if i > 0 {
			out.WriteByte(' ')
		}
		fmt.Fprintf(&out, "%s=%v", key, data[key])
	}
	return out.Bytes()
}

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
