package clientlog

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"

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
		level = slog.LevelDebug - slog.Level(verbosity-1)*4
	}
	options := &slog.HandlerOptions{AddSource: configuredCaller, Level: level}
	if configuredJSON {
		slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, options)))
	} else {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, options)))
	}
}

func Debugf(format string, args ...any) {
	logf(slog.LevelDebug, format, args...)
}

type VerboseLogger struct {
	level int
}

func V(level int) VerboseLogger {
	return VerboseLogger{level: level}
}

func (l VerboseLogger) Infof(format string, args ...any) {
	level := slog.LevelDebug - slog.Level(max(l.level-1, 0))*4
	logf(level, format, args...)
}

func logf(level slog.Level, format string, args ...any) {
	ctx := context.Background()
	if !slog.Default().Enabled(ctx, level) {
		return
	}
	slog.Log(ctx, level, fmt.Sprintf(format, args...))
}

func parseLevel(value string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "trace":
		return slog.LevelDebug - 4
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	case "info", "":
		return slog.LevelInfo
	default:
		if numeric, err := strconv.Atoi(value); err == nil && numeric > 0 {
			return slog.LevelDebug - slog.Level(numeric-1)*4
		}
		return slog.LevelInfo
	}
}
