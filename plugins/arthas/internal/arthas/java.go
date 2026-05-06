package arthas

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/flanksource/incident-commander/plugins/arthas/internal/k8s"
	"k8s.io/client-go/rest"
)

// JavaInfo describes the JVM found inside the target container.
type JavaInfo struct {
	// Major is the JVM major version (8, 11, 17, 21, ...).
	Major int
	// JavaHome is the value of $JAVA_HOME observed inside the container; may
	// be empty if the image doesn't set it.
	JavaHome string
	// HasToolsJar is true when $JAVA_HOME/lib/tools.jar (or ../lib/tools.jar)
	// exists. Only relevant for Java 8 — 9+ ships the attach API in base.
	HasToolsJar bool
}

// NeedsJDK reports whether arthas-boot will fail without a side-loaded JDK.
// True only for Java 8 on JRE-only images.
func (j JavaInfo) NeedsJDK() bool {
	return j.Major == 8 && !j.HasToolsJar
}

// detectJava inspects the target container to find the JVM version and
// whether tools.jar is available. Both pieces of information are needed to
// decide whether to side-load a JDK before attaching.
func detectJava(ctx context.Context, restCfg *rest.Config, opts StartOptions) (JavaInfo, error) {
	var stdout, stderr bytes.Buffer
	// java -version writes to stderr historically; merge streams to be safe.
	// Print JAVA_HOME and a tools.jar probe on stdout lines we can parse.
	script := `set -e
unset JAVA_TOOL_OPTIONS
echo "__JAVA_HOME__=${JAVA_HOME:-}"
if [ -n "${JAVA_HOME:-}" ] && { [ -f "$JAVA_HOME/lib/tools.jar" ] || [ -f "$JAVA_HOME/../lib/tools.jar" ]; }; then
  echo "__TOOLS_JAR__=yes"
else
  echo "__TOOLS_JAR__=no"
fi
java -version 2>&1`
	err := k8s.ExecInPod(ctx, restCfg, k8s.ExecOptions{
		Namespace: opts.Namespace,
		Pod:       opts.Pod,
		Container: opts.Container,
		Command:   []string{"sh", "-c", script},
		Stdout:    &stdout,
		Stderr:    &stderr,
	})
	if err != nil {
		return JavaInfo{}, fmt.Errorf("detect java: %w: stderr=%s", err, strings.TrimSpace(stderr.String()))
	}

	out := stdout.String()
	info := JavaInfo{
		JavaHome:    extractTagged(out, "__JAVA_HOME__="),
		HasToolsJar: extractTagged(out, "__TOOLS_JAR__=") == "yes",
	}

	major, err := parseJavaVersion(out)
	if err != nil {
		return JavaInfo{}, fmt.Errorf("%w (full output: %q)", err, out)
	}
	info.Major = major
	return info, nil
}

func extractTagged(output, tag string) string {
	for line := range strings.SplitSeq(output, "\n") {
		if rest, ok := strings.CutPrefix(line, tag); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

// Matches:
//
//	java version "1.8.0_412"          -> 8
//	openjdk version "1.8.0_412"       -> 8
//	openjdk version "11.0.21"         -> 11
//	openjdk version "17.0.9"          -> 17
//	openjdk 21.0.1 2023-10-17         -> 21   (no quotes on some distros)
var (
	quotedVersionRe   = regexp.MustCompile(`version "(\d+)(?:\.(\d+))?`)
	unquotedVersionRe = regexp.MustCompile(`(?m)^(?:openjdk|java)\s+(\d+)(?:\.|\s)`)
)

// parseJavaVersion pulls the major version out of the combined stdout+stderr
// of `java -version`. Returns an error if nothing matches.
func parseJavaVersion(output string) (int, error) {
	if m := quotedVersionRe.FindStringSubmatch(output); m != nil {
		first, _ := strconv.Atoi(m[1])
		// Legacy "1.X" form used up through Java 8.
		if first == 1 && m[2] != "" {
			second, _ := strconv.Atoi(m[2])
			return second, nil
		}
		return first, nil
	}
	if m := unquotedVersionRe.FindStringSubmatch(output); m != nil {
		return strconv.Atoi(m[1])
	}
	return 0, fmt.Errorf("could not parse java -version output")
}
