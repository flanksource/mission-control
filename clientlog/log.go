package clientlog

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/pflag"
)

var (
	levelCount   int
	levelName    = "info"
	jsonLogs     bool
	color        = true
	reportCaller bool
	logToStderr  = true
)

func BindFlags(flags *pflag.FlagSet) {
	flags.CountVarP(&levelCount, "loglevel", "v", "Increase logging level")
	flags.StringVar(&levelName, "log-level", "info", "Set the default log level")
	flags.BoolVar(&jsonLogs, "json-logs", false, "Print logs in json format to stderr")
	flags.BoolVar(&color, "color", true, "Retained for compatibility; lightweight logs are uncolored")
	flags.BoolVar(&reportCaller, "report-caller", false, "Report log caller info")
	flags.BoolVar(&logToStderr, "log-to-stderr", true, "Deprecated: logs always go to stderr")
}

func Configure(flags *pflag.FlagSet) {
	verbosity := levelCount
	configuredLevel := levelName
	configuredJSON := jsonLogs
	configuredCaller := reportCaller
	if flags != nil {
		if value, err := flags.GetCount("loglevel"); err == nil {
			verbosity = value
		}
		if value, err := flags.GetString("log-level"); err == nil {
			configuredLevel = value
		}
		if value, err := flags.GetBool("json-logs"); err == nil {
			configuredJSON = value
		}
		if value, err := flags.GetBool("report-caller"); err == nil {
			configuredCaller = value
		}
	}
	if environmentLevel := os.Getenv("LOG_LEVEL"); environmentLevel != "" && configuredLevel == "info" {
		configuredLevel = environmentLevel
	}
	level := parseLevel(configuredLevel)
	if verbosity > 0 {
		level = verbosityLevel(verbosity)
	}
	options := &slog.HandlerOptions{AddSource: configuredCaller, Level: level}
	if configuredJSON {
		slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, options)))
	} else {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, options)))
	}
}

func Debugf(format string, args ...any) {
	logf(slog.LevelDebug, "", format, args...)
}

func NamedDebugf(name, format string, args ...any) {
	logf(slog.LevelDebug, name, format, args...)
}

type VerboseLogger struct {
	level int
}

func V(level int) VerboseLogger {
	return VerboseLogger{level: level}
}

func (l VerboseLogger) Infof(format string, args ...any) {
	logf(verbosityLevel(l.level), "", format, args...)
}

func logf(level slog.Level, name, format string, args ...any) {
	ctx := context.Background()
	if !slog.Default().Enabled(ctx, level) {
		return
	}
	pcs := [1]uintptr{}
	runtime.Callers(3, pcs[:])
	record := slog.NewRecord(time.Now(), level, fmt.Sprintf(format, args...), pcs[0])
	if name != "" {
		record.AddAttrs(slog.String("logger", name))
	}
	_ = slog.Default().Handler().Handle(ctx, record)
}

func parseLevel(value string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "trace":
		return slog.LevelDebug - 1
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	case "fatal":
		return slog.LevelError + 1
	case "silent":
		return slog.Level(100)
	case "info", "":
		return slog.LevelInfo
	default:
		if trace, ok := strings.CutPrefix(strings.ToLower(value), "trace"); ok {
			if numeric, err := strconv.Atoi(trace); err == nil && numeric >= 0 {
				return slog.LevelDebug - 1 - slog.Level(numeric)
			}
		}
		if numeric, err := strconv.Atoi(value); err == nil {
			return verbosityLevel(numeric)
		}
		return slog.LevelInfo
	}
}

func verbosityLevel(value int) slog.Level {
	switch {
	case value <= -3:
		return slog.LevelError + 1
	case value == -2:
		return slog.LevelError
	case value == -1:
		return slog.LevelWarn
	case value == 0:
		return slog.LevelInfo
	case value == 1:
		return slog.LevelDebug
	default:
		return slog.LevelDebug - slog.Level(value-1)
	}
}
