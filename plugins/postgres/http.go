package main

import (
	"net/http"

	"github.com/flanksource/incident-commander/plugin/sdk"
)

func (p *PostgresPlugin) HTTPHandler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/version", sdk.VersionHandler(sdk.BuildInfo{
		Name:       "postgres",
		Version:    Version,
		BuildDate:  BuildDate,
		UIChecksum: uiChecksum,
	}))
	return mux
}
