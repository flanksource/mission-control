package cmd

import (
	"os"
	"os/signal"

	"github.com/flanksource/commons/logger"
)

var shutdownHooks []func()

func Shutdown() {
	if len(shutdownHooks) == 0 {
		return
	}
	logger.Infof("Shutting down %d", len(shutdownHooks))
	for _, fn := range shutdownHooks {
		fn()
	}
	shutdownHooks = []func(){}
}

func ShutdownAndExit(code int, msg string) {
	Shutdown()
	logger.StandardLogger().WithSkipReportLevel(1).Errorf(msg)
	os.Exit(code)
}

func AddShutdownHook(fn func()) {
	shutdownHooks = append(shutdownHooks, fn)
}

func WaitForShutdown() {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt)
	<-quit
	logger.Infof("Caught Ctrl+C")
	// call shutdown hooks explicitly, post-run cleanup hooks will be a no-op
	Shutdown()
}
