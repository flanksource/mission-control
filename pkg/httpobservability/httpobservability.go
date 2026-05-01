package httpobservability

import (
	nethttp "net/http"

	commonshttp "github.com/flanksource/commons/http"
	"github.com/flanksource/commons/logger"
)

// Apply wires a commons HTTP client into the named http logger.
func Apply(client *commonshttp.Client) *commonshttp.Client {
	if client == nil {
		return nil
	}
	return client.Use(func(rt nethttp.RoundTripper) nethttp.RoundTripper {
		return logger.NewHttpLoggerWithLevels(logger.GetLogger("http"), rt, logger.Debug, logger.Trace)
	})
}
