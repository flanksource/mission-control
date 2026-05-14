package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/flanksource/incident-commander/plugin/sdk"
)

func (p *SQLServerPlugin) httpTraceStream(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "trace id required", http.StatusBadRequest)
		return
	}
	trace, ok := p.traces.Get(id)
	if !ok {
		http.Error(w, fmt.Sprintf("trace %q not found", id), http.StatusNotFound)
		return
	}
	configID := sdk.ConfigItemIDFromContext(r.Context())
	if configID == "" || trace.ConfigItemID != configID {
		http.Error(w, "trace does not belong to this config", http.StatusForbidden)
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
