package clientlog

import (
	"context"
	"log/slog"
	"path/filepath"

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
		{name: "trace", value: "trace", expected: slog.LevelDebug - 1},
		{name: "trace level", value: "trace2", expected: slog.LevelDebug - 3},
		{name: "warning", value: "warn", expected: slog.LevelWarn},
		{name: "error", value: "error", expected: slog.LevelError},
		{name: "fatal", value: "fatal", expected: slog.LevelError + 1},
		{name: "silent", value: "silent", expected: slog.Level(100)},
		{name: "verbosity", value: "2", expected: slog.LevelDebug - 1},
		{name: "fallback", value: "invalid", expected: slog.LevelInfo},
	}

	for _, test := range tests {
		ginkgo.It(test.name, func() {
			Expect(parseLevel(test.value)).To(Equal(test.expected))
		})
	}

	ginkgo.It("preserves info severity at verbosity zero", func() {
		Expect(verbosityLevel(0)).To(Equal(slog.LevelInfo))
	})

	ginkgo.It("reports the migrated call site", func() {
		original := slog.Default()
		handler := &recordingHandler{}
		slog.SetDefault(slog.New(handler))
		defer slog.SetDefault(original)

		emitDebugFixture()

		Expect(filepath.Base(handler.record.Source().File)).To(Equal("log_test.go"))
		Expect(handler.record.Source().Function).To(ContainSubstring("emitDebugFixture"))
	})
})

func emitDebugFixture() {
	Debugf("fixture")
}

type recordingHandler struct {
	record slog.Record
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool {
	return true
}

func (h *recordingHandler) Handle(_ context.Context, record slog.Record) error {
	h.record = record.Clone()
	return nil
}

func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler {
	return h
}

func (h *recordingHandler) WithGroup(string) slog.Handler {
	return h
}
