package logs

import "github.com/flanksource/commons/logger"

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
