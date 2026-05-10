package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
)

// httpPods returns the pods for the catalog item passed in via query string.
// The frontend calls this to render the pod selector before opening a log
// stream.
func (p *KubernetesLogsPlugin) httpPods(w http.ResponseWriter, r *http.Request) {
	configID := r.URL.Query().Get("config_id")
	if configID == "" {
		http.Error(w, "config_id required", http.StatusBadRequest)
		return
	}

	// The host's identity is forwarded by the proxy as a header but the
	// HTTPHandler runs in the plugin process — it doesn't carry the gRPC
	// HostClient. The iframe operations that need host data must go via
	// the host's POST /operations endpoint (which does have HostClient
	// access). So this handler is best-effort: it works only when the
	// kubernetes connection is available via in-cluster fallback.
	cli, err := p.clients.For(r.Context(), nil)
	if err != nil {
		jsonError(w, http.StatusServiceUnavailable, err.Error())
		return
	}

	// Parse out kind/namespace/name from headers the host injects, if any.
	namespace := r.Header.Get("X-Mission-Control-Namespace")
	name := r.Header.Get("X-Mission-Control-Name")
	if namespace == "" || name == "" {
		jsonError(w, http.StatusBadRequest, "host did not forward namespace/name headers; use the gRPC list-pods operation instead")
		return
	}
	pods, err := podsBySelector(r.Context(), cli, namespace, map[string]string{"app": name})
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(pods)
}

// httpLogs streams pod logs as Server-Sent Events. One event per log line:
//
//	data: {"pod":"x","container":"c","line":"..."}\n\n
//
// The iframe consumes this via EventSource so logs appear live without
// re-polling. The TailLines query param caps the initial backlog; after
// that, follow=true streams new lines until the client disconnects.
func (p *KubernetesLogsPlugin) httpLogs(w http.ResponseWriter, r *http.Request) {
	pod := r.URL.Query().Get("pod")
	namespace := r.URL.Query().Get("namespace")
	container := r.URL.Query().Get("container")
	follow := r.URL.Query().Get("follow") != "false"
	tailLines, _ := strconv.ParseInt(r.URL.Query().Get("tailLines"), 10, 64)
	if tailLines <= 0 {
		tailLines = 200
	}
	if pod == "" || namespace == "" {
		http.Error(w, "pod and namespace required", http.StatusBadRequest)
		return
	}

	cli, err := p.clients.For(r.Context(), nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
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

	opts := &corev1.PodLogOptions{
		Container:  container,
		Follow:     follow,
		TailLines:  &tailLines,
		Timestamps: true,
	}
	stream, err := cli.CoreV1().Pods(namespace).GetLogs(pod, opts).Stream(r.Context())
	if err != nil {
		writeSSE(w, flusher, "error", err.Error())
		return
	}
	defer stream.Close()

	baseLabels := map[string]string{
		"namespace": namespace,
		"pod":       pod,
	}
	if container != "" {
		baseLabels["container"] = container
	}

	streamLines(r.Context(), stream, func(raw string) {
		writeSSEJSON(w, flusher, parseSSELine(pod, container, baseLabels, raw))
	})
}

// sseLogLine is the wire shape sent to the iframe over SSE. Pod/container/line
// stay flat for backwards compat with the existing renderer; timestamp and
// labels are added so the UI can show kubelet-reported time and any tags
// (`namespace`, `pod`, `container`) without the iframe having to re-derive
// them from the URL.
type sseLogLine struct {
	Pod       string            `json:"pod"`
	Container string            `json:"container"`
	Line      string            `json:"line"`
	Timestamp string            `json:"timestamp,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
}

// parseSSELine splits the leading RFC3339Nano timestamp produced by
// PodLogOptions.Timestamps from the message body and surfaces both, plus
// the per-stream labels.
func parseSSELine(pod, container string, baseLabels map[string]string, raw string) sseLogLine {
	out := sseLogLine{Pod: pod, Container: container, Labels: baseLabels}
	if idx := strings.IndexByte(raw, ' '); idx > 0 {
		if ts, err := time.Parse(time.RFC3339Nano, raw[:idx]); err == nil {
			out.Timestamp = ts.Format(time.RFC3339Nano)
			out.Line = raw[idx+1:]
			return out
		}
	}
	out.Line = raw
	return out
}

func streamLines(ctx context.Context, r interface{ Read([]byte) (int, error) }, fn func(string)) {
	scanner := bufio.NewScanner(struct{ Reader }{Reader{r}})
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}
		fn(strings.TrimRight(scanner.Text(), "\r"))
	}
}

// Reader adapts Read([]byte) to io.Reader so we can use bufio.Scanner.
type Reader struct {
	R interface{ Read([]byte) (int, error) }
}

func (r Reader) Read(p []byte) (int, error) { return r.R.Read(p) }

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

func jsonError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"error":` + strconv.Quote(msg) + `}`))
}
