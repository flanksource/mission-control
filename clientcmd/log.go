package clientcmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/flanksource/clicky"
	"github.com/flanksource/commons/logger"
)

func shouldLogJSON() bool {
	return clicky.Flags.JsonLogs || logger.IsJsonLogs()
}

func writeEvent(w io.Writer, data map[string]any) {
	_ = WriteEventOutput(w, "", data)
}

func WriteEventOutput(w io.Writer, file string, data map[string]any) error {
	var out []byte
	if shouldLogJSON() {
		out, _ = json.Marshal(data)
	} else {
		out = formatKeyValues(data)
	}
	out = append(out, '\n')

	if file != "" {
		return os.WriteFile(file, out, 0600)
	}
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
