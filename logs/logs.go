package logs

import (
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/timberio/go-datemath"
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
	FirstObserved time.Time         `json:"firstObserved,omitempty"`
	LastObserved  *time.Time        `json:"lastObserved,omitempty"`
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

type LogsRequestBase struct {
	// The start time for the query
	// SupportsDatemath
	Start string `json:"start,omitempty"`

	// The end time for the query
	// Supports Datemath
	End string `json:"end,omitempty"`

	// Limit is the maximum number of lines to return
	Limit string `json:"limit,omitempty" template:"true"`
}

func (r *LogsRequestBase) GetStart() (time.Time, error) {
	return datemath.ParseAndEvaluate(r.Start, datemath.WithNow(time.Now()))
}

func (r *LogsRequestBase) GetEnd() (time.Time, error) {
	return datemath.ParseAndEvaluate(r.End, datemath.WithNow(time.Now()))
}
