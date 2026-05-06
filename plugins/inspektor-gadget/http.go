package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/flanksource/incident-commander/plugin/sdk"
)

func (p *InspektorGadgetPlugin) HTTPHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/sessions/", p.httpSession)
	mux.Handle("/version", sdk.VersionHandler(sdk.BuildInfo{
		Name:       pluginName,
		Version:    Version,
		BuildDate:  BuildDate,
		UIChecksum: uiChecksum,
	}))
	return mux
}

func (p *InspektorGadgetPlugin) httpSession(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/sessions/")
	id, tail, _ := strings.Cut(rest, "/")
	if id == "" {
		http.Error(w, "missing session id", http.StatusBadRequest)
		return
	}
	sess, ok := p.sessions.Get(id)
	if !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	switch tail {
	case "events":
		streamSessionEvents(w, r, sess)
	case "export":
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", id+".ndjson"))
		for _, event := range sess.Events() {
			b, _ := json.Marshal(event)
			fmt.Fprintf(w, "%s\n", b)
		}
	default:
		http.NotFound(w, r)
	}
}

func streamSessionEvents(w http.ResponseWriter, r *http.Request, sess *TraceSession) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	for _, event := range sess.Events() {
		writeSSEJSON(w, flusher, event)
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-sess.events:
			if !ok {
				writeSSE(w, flusher, "done", "{}")
				return
			}
			writeSSEJSON(w, flusher, event)
		}
	}
}

func writeSSE(w http.ResponseWriter, f http.Flusher, event, data string) {
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
	f.Flush()
}

func writeSSEJSON(w http.ResponseWriter, f http.Flusher, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", b)
	f.Flush()
}
