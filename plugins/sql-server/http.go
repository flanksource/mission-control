package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/flanksource/incident-commander/plugin/sdk"
)

// HTTPHandler powers the iframe's streaming endpoints. The unary
// operations (stats/query/explain/trace-*) are handled by the host's
// /api/plugins/<name>/operations/<op> route which has HostClient access;
// SSE has to live in the plugin's HTTP server because operations are
// unary RPCs.
//
// /trace-stream/<id> tails an existing trace's event buffer.
func (p *SQLServerPlugin) HTTPHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/trace-stream/", p.httpTraceStream)
	mux.Handle("/version", sdk.VersionHandler(sdk.BuildInfo{
		Name:       "sql-server",
		Version:    Version,
		BuildDate:  BuildDate,
		UIChecksum: uiChecksum,
	}))
	return mux
}

func (p *SQLServerPlugin) httpTraceStream(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/trace-stream/")
	if id == "" {
		http.Error(w, "trace id required", http.StatusBadRequest)
		return
	}
	trace, ok := p.traces.Get(id)
	if !ok {
		http.Error(w, fmt.Sprintf("trace %q not found", id), http.StatusNotFound)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Tail loop: ship every new event as one SSE frame, terminate when the
	// trace stops, the client disconnects, or the server is shutting down.
	since := r.URL.Query().Get("since")
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
		}
		events := trace.EventsSince(since)
		for _, e := range events {
			b, err := json.Marshal(e)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", b)
			flusher.Flush()
			since = e.Key()
		}
		if !trace.Running() {
			fmt.Fprintf(w, "event: done\ndata: {}\n\n")
			flusher.Flush()
			return
		}
	}
}
