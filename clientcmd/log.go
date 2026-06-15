package clientcmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"github.com/flanksource/clicky"
	"github.com/flanksource/commons/logger"
	"sigs.k8s.io/yaml"
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

func LogYAML(w io.Writer, data map[string]any) error {
	if shouldLogJSON() {
		return Log(w, data)
	}

	out, err := yaml.Marshal(data)
	if err != nil {
		return err
	}
	_, err = w.Write(out)
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
