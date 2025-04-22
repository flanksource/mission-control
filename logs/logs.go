package logs

import (
	"github.com/flanksource/commons/logger"
)

// IfError logs the given error message if there's an error.
func IfError(err error, description string) {
	IfErrorf(err, "%s: %v", description)
}

// IfErrorf logs the given error message if there's an error.
// The formatted string receives the error as an additional arg.
func IfErrorf(err error, format string, args ...any) {
	if err != nil {
		logger.Errorf(format, append(args, err)...)
	}
}

type LogLine struct {
	ID            string            `json:"id,omitempty"`
	FirstObserved string            `json:"firstObserved,omitempty"`
	LastObserved  string            `json:"lastObserved,omitempty"`
	Count         int               `json:"count,omitempty"`
	Message       string            `json:"message"`
	Hash          string            `json:"hash,omitempty"`
	Severity      string            `json:"severity,omitempty"`
	Source        string            `json:"source,omitempty"`
	Host          string            `json:"host,omitempty"`
	Labels        map[string]string `json:"labels,omitempty"`
}

type LogResult struct {
	Metadata map[string]any `json:"metadata,omitempty"`
	Logs     []LogLine      `json:"logs,omitempty"`
}
