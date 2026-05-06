package main

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"

	"github.com/flanksource/incident-commander/plugin/sdk"
)

func (p *GolangPlugin) HTTPHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/pprof/", p.httpProxyPprof)
	mux.HandleFunc("/profiles/", p.httpProfile)
	mux.Handle("/version", sdk.VersionHandler(sdk.BuildInfo{
		Name:       pluginName,
		Version:    Version,
		BuildDate:  BuildDate,
		UIChecksum: uiChecksum,
	}))
	return mux
}

func (p *GolangPlugin) httpProxyPprof(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/pprof/")
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
	if !sess.PprofAvailable || sess.PprofLocal == 0 {
		http.Error(w, "pprof is not available for this session", http.StatusBadRequest)
		return
	}
	target, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", sess.PprofLocal))
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ErrorLog = log.New(os.Stderr, "[WARN] golang pprof proxy: ", 0)
	base := strings.TrimRight(normalizePprofBase(sess.PprofBasePath), "/")
	r.URL.Path = base
	if tail != "" {
		r.URL.Path += "/" + tail
	}
	r.URL.RawPath = ""
	proxy.ServeHTTP(w, r)
}

func (p *GolangPlugin) httpProfile(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/profiles/")
	id, kind, _ := strings.Cut(rest, "/")
	kind = normalizeProfileKind(kind)
	if id == "" || kind == "" {
		http.Error(w, "expected /profiles/{sessionID}/{heap|cpu|trace}", http.StatusBadRequest)
		return
	}
	sess, ok := p.sessions.Get(id)
	if !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	data, source, err := collectProfile(r.Context(), sess, kind, p.settings.MaxProfileSec)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	filename := fmt.Sprintf("%s-%s.%s", pluginName, id, profileExtension(kind))
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, filename))
	w.Header().Set("X-Diagnostics-Source", source)
	w.Header().Set("Content-Type", profileContentType(kind))
	_, _ = w.Write(data)
}

func profileExtension(kind string) string {
	if kind == "trace" {
		return "trace"
	}
	return "pprof"
}

func profileContentType(kind string) string {
	if kind == "trace" {
		return "application/octet-stream"
	}
	return "application/vnd.google.protobuf"
}
