package utils

import "github.com/flanksource/commons/logger"

func LogIfError(err error, description string) {
	if err != nil {
		logger.Errorf("%s: %v", description, err)
	}
}
