package arthas

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"

	"github.com/flanksource/incident-commander/plugins/arthas/internal/k8s"
	"k8s.io/client-go/rest"
)

// pickRemoteTelnetPort returns a high ephemeral port for arthas' in-pod telnet
// listener. arthas-boot probes this port before attaching and bails if
// anything answers — so using a fresh random port per session avoids clashes
// with unrelated listeners already in the target JVM (some Spring/Tomcat
// images bind arthas' default 3658 for JMX/debug). Range 40000-49999.
func pickRemoteTelnetPort() int {
	var b [2]byte
	_, _ = rand.Read(b[:])
	return 40000 + int(binary.BigEndian.Uint16(b[:])%10000)
}

const (
	ArthasJarPath     = "/tmp/arthas-boot.jar"
	ArthasBootURL     = "https://arthas.aliyun.com/arthas-boot.jar"
	DefaultRemoteHTTP = 8563
	DefaultRemoteMCP  = 8777
)

// portProbePrelude is a POSIX-sh helper injected at the top of every exec
// script. It defines probe_port(host, port) which returns 0 if the port is
// reachable. We can't rely on /dev/tcp because many slim JDK base images
// use dash or ash, which lack that bash-ism — so prefer curl (robust on
// IPv6-only listeners common with Java NIO) and fall back to bash/python.
const portProbePrelude = `probe_port() {
  _host="$1" ; _port="$2"
  if command -v curl >/dev/null 2>&1; then
    case $(curl -s -o /dev/null -w "%%{http_code}" --connect-timeout 2 "http://$_host:$_port/") in
      000) return 1 ;;
      *)   return 0 ;;
    esac
  fi
  if command -v bash >/dev/null 2>&1; then
    bash -c "(echo > /dev/tcp/$_host/$_port) >/dev/null 2>&1" && return 0
    return 1
  fi
  if command -v python3 >/dev/null 2>&1 || command -v python >/dev/null 2>&1; then
    _py=$(command -v python3 || command -v python)
    "$_py" -c "import socket,sys; s=socket.create_connection(('$_host',int($_port)),2); s.close()" >/dev/null 2>&1 && return 0
    return 1
  fi
  echo "no port-probe tool available (need curl, bash, or python)" >&2
  return 2
}
`

// StartOptions captures everything needed to attach Arthas to a JVM in a pod.
type StartOptions struct {
	Namespace  string
	Kind       string
	Name       string
	Pod        string
	Container  string
	LocalHTTP  int // 0 = auto-allocate
	LocalMCP   int // 0 = auto-allocate
	RemoteHTTP int // defaults to 8563
	RemoteMCP  int // defaults to 8777
	// SkipJDKInstall bypasses the Java 8 JRE side-load; the attach will fail
	// loud if tools.jar is missing. Intended for operators who've provisioned
	// the pod out-of-band.
	SkipJDKInstall bool
}

// Start runs the full bootstrap: ensure Arthas jar is present, attach it to
// PID 1 with HTTP enabled, start the MCP server plugin, and open port-forwards
// to the caller's workstation. The returned Session carries a Stop closure
// that tears down the port-forwards. Note: the in-pod Arthas process is left
// running; a subsequent Start on the same pod will reuse it.
func Start(ctx context.Context, restCfg *rest.Config, opts StartOptions) (*Session, error) {
	if opts.RemoteHTTP == 0 {
		opts.RemoteHTTP = DefaultRemoteHTTP
	}
	if opts.RemoteMCP == 0 {
		opts.RemoteMCP = DefaultRemoteMCP
	}

	javaInfo, err := detectJava(ctx, restCfg, opts)
	if err != nil {
		return nil, fmt.Errorf("detect java: %w", err)
	}

	if err := ensureArthas(ctx, restCfg, opts); err != nil {
		return nil, fmt.Errorf("install arthas: %w", err)
	}

	var javaHome string
	if !opts.SkipJDKInstall {
		javaHome, err = ensureJDK(ctx, restCfg, opts, javaInfo)
		if err != nil {
			return nil, err
		}
	}

	remoteTelnet := pickRemoteTelnetPort()
	if err := attachArthas(ctx, restCfg, opts, javaHome, remoteTelnet); err != nil {
		return nil, fmt.Errorf("attach arthas: %w", err)
	}
	// MCP plugin is distributed separately from arthas-boot and isn't present
	// in upstream arthas 4.1.x. Attempt to start it — if the command doesn't
	// exist, fall through: the arthas HTTP /api endpoint on :8563 is itself a
	// fully-featured REST surface the UI exposes directly, so MCP is a nice-
	// to-have, not a hard requirement.
	mcpEnabled := true
	if err := enableMCP(ctx, restCfg, opts); err != nil {
		mcpEnabled = false
	}
	_ = mcpEnabled // surfaced to Session below

	localHTTP, err := pickLocalPort(opts.LocalHTTP)
	if err != nil {
		return nil, err
	}
	mappings := []k8s.PortMapping{{LocalPort: localHTTP, RemotePort: opts.RemoteHTTP}}
	localMCP := 0
	if mcpEnabled {
		localMCP, err = pickLocalPort(opts.LocalMCP)
		if err != nil {
			return nil, err
		}
		mappings = append(mappings, k8s.PortMapping{LocalPort: localMCP, RemotePort: opts.RemoteMCP})
	}

	fwd, ready, err := k8s.StartPortForward(restCfg, opts.Namespace, opts.Pod, mappings, io.Discard, io.Discard)
	if err != nil {
		return nil, fmt.Errorf("port-forward: %w", err)
	}
	if err := fwd.Ready(ctx, ready); err != nil {
		_ = fwd.Close()
		return nil, fmt.Errorf("port-forward not ready: %w", err)
	}

	sess := NewSession(opts.Namespace, opts.Kind, opts.Name, opts.Pod, opts.Container, localHTTP, localMCP, func() error {
		return fwd.Close()
	})
	sess.JavaVersion = javaInfo.Major
	sess.JDKProvisioned = javaHome != ""
	sess.SideloadedJavaHome = javaHome
	sess.MCPEnabled = mcpEnabled
	return sess, nil
}

func ensureArthas(ctx context.Context, restCfg *rest.Config, opts StartOptions) error {
	// Skip download if jar already exists.
	script := fmt.Sprintf(`set -e
if [ -f %[1]s ]; then exit 0; fi
if command -v curl >/dev/null 2>&1; then
  curl -fsSL %[2]s -o %[1]s
elif command -v wget >/dev/null 2>&1; then
  wget -q %[2]s -O %[1]s
else
  echo "neither curl nor wget available in container" >&2
  exit 127
fi`, ArthasJarPath, ArthasBootURL)
	return execSh(ctx, restCfg, opts, script)
}

func attachArthas(ctx context.Context, restCfg *rest.Config, opts StartOptions, javaHome string, remoteTelnet int) error {
	// Unset JAVA_TOOL_OPTIONS for the whole attach flow: the pod's JVM
	// javaagents (e.g. jmx_exporter, otel) are frequently incompatible with
	// the JDK tools we invoke here (jps, the arthas-boot launcher), and cause
	// them to abort in premain.
	//
	// If javaHome is non-empty we prefer it for both PID discovery and the
	// arthas-boot launcher. This is the Java-8-JRE side-load path.
	javaHomeExport := ""
	if javaHome != "" {
		javaHomeExport = fmt.Sprintf("export JAVA_HOME=%s\nexport PATH=\"$JAVA_HOME/bin:$PATH\"", javaHome)
	}
	script := fmt.Sprintf(`set -e
unset JAVA_TOOL_OPTIONS
%[3]s
`+portProbePrelude+`
# Happy path: arthas HTTP is already up (previous run).
if probe_port 127.0.0.1 %[1]d; then exit 0; fi

# arthas-boot probes the chosen telnet port before attaching; if anything
# answers it bails with "already listen port ..., skip attach". Some base
# images already bind 3658 for other reasons, so we pick a fresh high port
# per session (the telnet listener stays loopback-only anyway — we only
# port-forward HTTP + MCP to the caller).
if probe_port 127.0.0.1 %[4]d; then
  echo "chosen telnet port %[4]d is already in use in the pod; retry to pick a different port" >&2
  exit 1
fi

# Discover the target JVM. Try jps first; fall back to scanning /proc for a
# 'java' command line (jps can itself crash if the pod's JDK is weird, or may
# be absent on a JRE-only image).
PID=""
if command -v jps >/dev/null 2>&1; then
  PID=$(jps -l 2>/dev/null | awk '$2 != "sun.tools.jps.Jps" && $2 != "jdk.jcmd/sun.tools.jps.Jps" && $1 ~ /^[0-9]+$/ {print $1; exit}')
fi
if [ -z "$PID" ]; then
  for d in /proc/[0-9]*; do
    [ -r "$d/comm" ] || continue
    case "$(cat "$d/comm" 2>/dev/null)" in
      java) PID=$(basename "$d"); break ;;
    esac
  done
fi
if [ -z "$PID" ]; then
  echo "no JVM process found (jps failed and /proc scan found no 'java' command)" >&2
  exit 1
fi

# Clear any previous log so we don't surface stale errors from earlier attempts.
: > /tmp/arthas.log
# Launcher script: unset JAVA_TOOL_OPTIONS and (optionally) point JAVA_HOME at
# the side-loaded JDK, then exec java so the JVM process inherits an env
# without JAVA_TOOL_OPTIONS. Robust even on shells where var-prefix / 'env -u'
# leaks through nohup.
cat >/tmp/arthas-launch.sh <<EOF
#!/bin/sh
unset JAVA_TOOL_OPTIONS
%[3]s
exec java -jar %[2]s --attach-only --target-ip 127.0.0.1 --http-port %[1]d --telnet-port %[4]d "$PID"
EOF
chmod +x /tmp/arthas-launch.sh
nohup sh /tmp/arthas-launch.sh >/tmp/arthas.log 2>&1 &
# Wait up to 60s for the HTTP port to come up. arthas-core starts listeners
# in a separate thread after agent attach, so this is the race-window.
for i in $(seq 1 120); do
  if probe_port 127.0.0.1 %[1]d; then exit 0; fi
  sleep 0.5
done
echo "arthas HTTP port %[1]d did not come up; see /tmp/arthas.log in the pod" >&2
head -n 40 /tmp/arthas.log >&2 || true
exit 1`, opts.RemoteHTTP, ArthasJarPath, javaHomeExport, remoteTelnet)
	return execSh(ctx, restCfg, opts, script)
}

func enableMCP(ctx context.Context, restCfg *rest.Config, opts StartOptions) error {
	// Use Arthas HTTP API to start the mcp-server plugin.
	// https://arthas.aliyun.com/en/doc/http-api.html
	script := fmt.Sprintf(`set -e
`+portProbePrelude+`
if probe_port 127.0.0.1 %[2]d; then exit 0; fi
RESP=$(curl -sS -XPOST "http://127.0.0.1:%[1]d/api" \
  -H 'Content-Type: application/json' \
  -d '{"action":"exec","command":"mcp-server start --port %[2]d"}')
echo "$RESP"
echo "$RESP" | grep -q '"state":"SUCCEEDED"' || {
  echo "mcp-server plugin did not start (needs Arthas with MCP support)" >&2
  exit 1
}
for i in $(seq 1 20); do
  if probe_port 127.0.0.1 %[2]d; then exit 0; fi
  sleep 0.5
done
echo "mcp port %[2]d did not come up" >&2
exit 1`, opts.RemoteHTTP, opts.RemoteMCP)
	return execSh(ctx, restCfg, opts, script)
}

func execSh(ctx context.Context, restCfg *rest.Config, opts StartOptions, script string) error {
	var stdout, stderr bytes.Buffer
	err := k8s.ExecInPod(ctx, restCfg, k8s.ExecOptions{
		Namespace: opts.Namespace,
		Pod:       opts.Pod,
		Container: opts.Container,
		Command:   []string{"sh", "-c", script},
		Stdout:    &stdout,
		Stderr:    &stderr,
	})
	if err != nil {
		return fmt.Errorf("%w: stderr=%s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func pickLocalPort(preferred int) (int, error) {
	if preferred > 0 {
		return preferred, nil
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("allocate local port: %w", err)
	}
	addr := l.Addr().String()
	if cerr := l.Close(); cerr != nil {
		return 0, fmt.Errorf("release probe listener: %w", cerr)
	}
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(portStr)
}
