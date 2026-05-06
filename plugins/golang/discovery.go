package main

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"

	golangk8s "github.com/flanksource/incident-commander/plugins/golang/internal/k8s"
	"k8s.io/client-go/rest"
)

type GopsProcess struct {
	PID     int    `json:"pid"`
	Port    int    `json:"port"`
	Command string `json:"command,omitempty"`
}

func discoverGopsProcesses(ctx context.Context, restCfg *rest.Config, namespace, pod, container string, dirs []string) ([]GopsProcess, error) {
	script := buildGopsDiscoveryScript(dirs)
	var stdout, stderr bytes.Buffer
	if err := golangk8s.ExecInPod(ctx, restCfg, golangk8s.ExecOptions{
		Namespace: namespace,
		Pod:       pod,
		Container: container,
		Command:   []string{"sh", "-c", script},
		Stdout:    &stdout,
		Stderr:    &stderr,
	}); err != nil {
		return nil, fmt.Errorf("discover gops ports: %w (stderr: %s)", err, strings.TrimSpace(stderr.String()))
	}
	return parseGopsDiscovery(stdout.String()), nil
}

func buildGopsDiscoveryScript(dirs []string) string {
	quoted := make([]string, 0, len(dirs)+1)
	quoted = append(quoted, `"${GOPS_CONFIG_DIR:-}"`)
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		quoted = append(quoted, shellQuote(dir))
	}
	return fmt.Sprintf(`set +e
for dir in %s; do
  [ -n "$dir" ] || continue
  for expanded in $dir; do
    [ -d "$expanded" ] || continue
    for f in "$expanded"/*; do
      [ -f "$f" ] || continue
      pid=$(basename "$f")
      case "$pid" in ""|*[!0-9]*) continue ;; esac
      port=$(cat "$f" 2>/dev/null | tr -dc '0-9')
      [ -n "$port" ] || continue
      [ -d "/proc/$pid" ] || continue
      cmd=""
      if [ -r "/proc/$pid/cmdline" ]; then
        cmd=$(tr '\000' ' ' <"/proc/$pid/cmdline" 2>/dev/null)
      fi
      printf 'pid=%%s port=%%s cmd=%%s\n' "$pid" "$port" "$cmd"
    done
  done
done
`, strings.Join(quoted, " "))
}

func parseGopsDiscovery(raw string) []GopsProcess {
	var out []GopsProcess
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var proc GopsProcess
		for _, field := range strings.Fields(line) {
			k, v, ok := strings.Cut(field, "=")
			if !ok {
				continue
			}
			switch k {
			case "pid":
				proc.PID, _ = strconv.Atoi(v)
			case "port":
				proc.Port, _ = strconv.Atoi(v)
			}
		}
		if idx := strings.Index(line, " cmd="); idx >= 0 {
			proc.Command = strings.TrimSpace(line[idx+5:])
		}
		if proc.PID > 0 && proc.Port > 0 {
			out = append(out, proc)
		}
	}
	return out
}

func selectGopsProcess(processes []GopsProcess, pid int) (GopsProcess, bool) {
	if pid > 0 {
		for _, proc := range processes {
			if proc.PID == pid {
				return proc, true
			}
		}
		return GopsProcess{}, false
	}
	if len(processes) == 0 {
		return GopsProcess{}, false
	}
	return processes[0], true
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
