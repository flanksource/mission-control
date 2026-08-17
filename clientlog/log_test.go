package clientlog

import (
	"log/slog"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("log levels", func() {
	tests := []struct {
		name     string
		value    string
		expected slog.Level
	}{
		{name: "info", value: "info", expected: slog.LevelInfo},
		{name: "debug", value: "debug", expected: slog.LevelDebug},
		{name: "trace", value: "trace", expected: slog.LevelDebug - 4},
		{name: "warning", value: "warn", expected: slog.LevelWarn},
		{name: "error", value: "error", expected: slog.LevelError},
		{name: "verbosity", value: "2", expected: slog.LevelDebug - 4},
		{name: "fallback", value: "invalid", expected: slog.LevelInfo},
	}

	for _, test := range tests {
		ginkgo.It(test.name, func() {
			Expect(parseLevel(test.value)).To(Equal(test.expected))
		})
	}
})
