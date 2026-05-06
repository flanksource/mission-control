package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"

	"github.com/flanksource/incident-commander/plugin/sdk"
)

func (p *ArthasPlugin) HTTPHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/proxy/", p.httpProxyConsole)
	mux.HandleFunc("/mcp/", p.httpProxyMCP)
	mux.Handle("/version", sdk.VersionHandler(sdk.BuildInfo{
		Name:       "arthas",
		Version:    Version,
		BuildDate:  BuildDate,
		UIChecksum: uiChecksum,
	}))
	return mux
}

func (p *ArthasPlugin) httpProxyConsole(w http.ResponseWriter, r *http.Request) {
	p.proxyTo(w, r, "/proxy/", func(s sessionPorts) int { return s.HTTP }, true)
}

func (p *ArthasPlugin) httpProxyMCP(w http.ResponseWriter, r *http.Request) {
	p.proxyTo(w, r, "/mcp/", func(s sessionPorts) int { return s.MCP }, false)
}

type sessionPorts struct {
	HTTP int
	MCP  int
}

func (p *ArthasPlugin) proxyTo(w http.ResponseWriter, r *http.Request, prefix string, portOf func(sessionPorts) int, rewriteHTML bool) {
	rest := strings.TrimPrefix(r.URL.Path, prefix)
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
	port := portOf(sessionPorts{HTTP: sess.HTTPLocalPort, MCP: sess.MCPLocalPort})
	if port == 0 {
		http.Error(w, "session endpoint is not enabled", http.StatusBadRequest)
		return
	}
	target, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ErrorLog = log.New(os.Stderr, "[WARN] arthas console proxy: ", 0)
	if rewriteHTML {
		// The browser sees this iframe behind the host's UI proxy, so absolute
		// URLs we inject must include the X-Forwarded-Prefix the host set.
		hostPrefix := strings.TrimRight(r.Header.Get("X-Forwarded-Prefix"), "/")
		basePrefix := fmt.Sprintf("%s%s%s/", hostPrefix, prefix, id)
		proxy.ModifyResponse = func(resp *http.Response) error {
			return rewriteArthasResponse(resp, basePrefix)
		}
	}
	r.URL.Path = "/" + tail
	r.URL.RawPath = ""
	proxy.ServeHTTP(w, r)
}

func rewriteArthasResponse(resp *http.Response, basePrefix string) error {
	rewriteLocation(resp, basePrefix)

	ctype := resp.Header.Get("Content-Type")
	isHTML := strings.Contains(ctype, "text/html")
	isJS := strings.Contains(ctype, "javascript")
	isCSS := strings.Contains(ctype, "text/css")
	if !isHTML && !isJS && !isCSS {
		return nil
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()

	proxyRoot := strings.TrimSuffix(basePrefix, "/")
	if isHTML {
		body = rewriteHTMLRootPaths(body, basePrefix)
		wsShim := fmt.Appendf(nil, `<script>(function(){
  var Orig = window.WebSocket;
  var proxyBase = %q;
  var proxyRoot = %q;
  function rewriteRootPath(value) {
    try {
      var u = new URL(value, window.location.href);
      if (u.origin === window.location.origin && (u.pathname === "/api" || u.pathname.indexOf("/api/") === 0 || u.pathname === "/ws" || u.pathname.indexOf("/ws/") === 0 || u.pathname.indexOf("/static/") === 0)) {
        return proxyRoot + u.pathname + u.search + u.hash;
      }
    } catch(e) {}
    return value;
  }
  var origFetch = window.fetch;
  window.fetch = function(input, init) {
    if (typeof input === "string") {
      input = rewriteRootPath(input);
    } else if (input && input.url) {
      var next = rewriteRootPath(input.url);
      if (next !== input.url) input = new Request(next, input);
    }
    return origFetch.call(this, input, init);
  };
  var origOpen = XMLHttpRequest.prototype.open;
  XMLHttpRequest.prototype.open = function(method, url) {
    arguments[1] = rewriteRootPath(url);
    return origOpen.apply(this, arguments);
  };
  window.WebSocket = function(url, protocols){
    try {
      var u = new URL(url, window.location.href);
      if (u.pathname === "/ws" || u.pathname.endsWith("/ws")) {
        var proto = window.location.protocol === "https:" ? "wss:" : "ws:";
        url = proto + "//" + window.location.host + proxyBase + "ws";
      }
    } catch(e) {}
    return protocols ? new Orig(url, protocols) : new Orig(url);
  };
  window.WebSocket.prototype = Orig.prototype;
  window.WebSocket.CONNECTING = Orig.CONNECTING;
  window.WebSocket.OPEN = Orig.OPEN;
  window.WebSocket.CLOSING = Orig.CLOSING;
  window.WebSocket.CLOSED = Orig.CLOSED;
})();</script>`, basePrefix, proxyRoot)
		idx := bytes.Index(body, []byte("<head>"))
		if idx < 0 {
			idx = bytes.Index(bytes.ToLower(body), []byte("<head>"))
		}
		if idx >= 0 {
			insertAt := idx + len("<head>")
			base := fmt.Appendf(nil, `<base href=%q>`, basePrefix)
			inject := append(base, wsShim...)
			body = append(body[:insertAt], append(inject, body[insertAt:]...)...)
		}
	}
	if isJS {
		body = rewriteScriptRootPaths(body, proxyRoot)
	}
	if isCSS {
		body = rewriteCSSRootPaths(body, proxyRoot)
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.ContentLength = int64(len(body))
	resp.Header.Set("Content-Length", fmt.Sprint(len(body)))
	return nil
}

func rewriteLocation(resp *http.Response, basePrefix string) {
	loc := resp.Header.Get("Location")
	if loc == "" {
		return
	}
	proxyRoot := strings.TrimSuffix(basePrefix, "/")
	for _, root := range []string{"/static/", "/api", "/ws"} {
		if loc == root || strings.HasPrefix(loc, root+"/") || strings.HasPrefix(loc, root) && strings.HasSuffix(root, "/") {
			resp.Header.Set("Location", proxyRoot+loc)
			return
		}
	}
}

func rewriteHTMLRootPaths(body []byte, basePrefix string) []byte {
	proxyRoot := strings.TrimSuffix(basePrefix, "/")
	attrs := []string{"src", "href", "action", "poster", "data", "content"}
	for _, attr := range attrs {
		for _, quote := range []byte{'"', '\''} {
			for _, root := range proxiedRoots() {
				old := []byte(fmt.Sprintf(`%s=%c%s`, attr, quote, root))
				newValue := proxyRoot + root
				body = bytes.ReplaceAll(body, old, []byte(fmt.Sprintf(`%s=%c%s`, attr, quote, newValue)))
			}
		}
	}
	body = rewriteCSSRootPaths(body, proxyRoot)
	return body
}

func rewriteScriptRootPaths(body []byte, proxyRoot string) []byte {
	for _, quote := range []byte{'"', '\'', '`'} {
		for _, root := range proxiedRoots() {
			old := []byte{quote}
			old = append(old, root...)
			newValue := []byte{quote}
			newValue = append(newValue, proxyRoot...)
			newValue = append(newValue, root...)
			body = bytes.ReplaceAll(body, old, newValue)
		}
	}
	return body
}

func rewriteCSSRootPaths(body []byte, proxyRoot string) []byte {
	for _, root := range proxiedRoots() {
		body = bytes.ReplaceAll(body, []byte("url("+root), []byte("url("+proxyRoot+root))
		body = bytes.ReplaceAll(body, []byte(`url("`+root), []byte(`url("`+proxyRoot+root))
		body = bytes.ReplaceAll(body, []byte(`url('`+root), []byte(`url('`+proxyRoot+root))
	}
	return body
}

func proxiedRoots() []string {
	return []string{"/api", "/ws", "/static/"}
}
