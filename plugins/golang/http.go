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
	id, tail, _ := strings.Cut(rest, "/")
	if id == "" || tail == "" {
		http.Error(w, "expected /profiles/{sessionID}/{runID|heap|cpu|trace}", http.StatusBadRequest)
		return
	}
	sess, ok := p.sessions.Get(id)
	if !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	runIDOrKind, subPath, _ := strings.Cut(tail, "/")
	if run, ok := p.profiles.Get(runIDOrKind); ok {
		if run.SessionID != sess.ID {
			http.Error(w, "profile run does not belong to session", http.StatusForbidden)
			return
		}
		snapshot := run.Snapshot()
		if snapshot.State != "completed" {
			http.Error(w, "profile run is not completed", http.StatusConflict)
			return
		}
		if subPath != "" {
			p.proxyProfileViewer(w, r, sess, run, subPath)
			return
		}
		data := run.Data()
		if len(data) == 0 {
			http.Error(w, "profile run has no data", http.StatusNotFound)
			return
		}
		writeProfileDownload(w, sess.ID, run.ID, snapshot.Kind, snapshot.Source, data)
		return
	}

	kind := normalizeProfileKind(runIDOrKind)
	if kind == "" {
		http.Error(w, "expected /profiles/{sessionID}/{runID|heap|cpu|trace}", http.StatusBadRequest)
		return
	}
	data, source, err := collectProfile(r.Context(), sess, kind, p.settings.MaxProfileSec)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeProfileDownload(w, sess.ID, kind, kind, source, data)
}

func (p *GolangPlugin) proxyProfileViewer(w http.ResponseWriter, r *http.Request, _ *Session, run *ProfileRun, subPath string) {
	if p.viewers == nil {
		http.Error(w, "profile viewer registry is not initialised", http.StatusInternalServerError)
		return
	}
	addr, err := p.viewers.Get(r.Context(), run)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	target, _ := url.Parse("http://" + addr)
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ErrorLog = log.New(os.Stderr, "[WARN] golang profile viewer: ", 0)
	r.URL.Path = "/" + strings.TrimLeft(subPath, "/")
	r.URL.RawPath = ""
	proxy.ServeHTTP(w, r)
}

func writeProfileDownload(w http.ResponseWriter, sessionID, name, kind, source string, data []byte) {
	filename := fmt.Sprintf("%s-%s-%s.%s", pluginName, sessionID, name, profileExtension(kind))
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
