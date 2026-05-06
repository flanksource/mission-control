// Package decompile drives the `arthas decompile` subcommand: it sends
// `jad <Class>` commands to a running Arthas session, parses the
// `/*N*/`-prefixed output back into a line→source map, caches results on
// disk by container image digest, and enriches `models.Frame` instances with
// a window of source lines around each frame's reported line number.
package decompile

import (
	"regexp"
	"strconv"
	"strings"
)

// linePrefixRegexp matches Arthas's `/*<lineNumber>*/` line-number prefix.
// Anchored to the start of the line; everything after the closing `*/` is the
// source for that line.
var linePrefixRegexp = regexp.MustCompile(`^\s*/\*\s*(\d+)\s*\*/(.*)$`)

// ParseJad parses the body returned by Arthas's `jad <class>` command into a
// line-number-keyed map of source lines.
//
// Arthas emits a fixed prologue (ClassLoader / Location blocks plus a CFR
// banner comment), then the decompiled body where each line carries a
// `/*<bytecodeLine>*/` prefix, followed by an `Affect(...)` summary. Lines
// without a prefix in the body are continuation lines and are appended to the
// previous numbered line. Empty lines and the `/*EOF*/` marker are skipped.
//
// Returns the empty map for empty input — callers should treat that as
// "decompile produced no source", not an error.
func ParseJad(text string) map[int]string {
	out := make(map[int]string)
	if strings.TrimSpace(text) == "" {
		return out
	}

	inBody := false
	lastLine := 0
	for _, raw := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "/*EOF*/" {
			continue
		}
		if strings.HasPrefix(trimmed, "Affect(") {
			break
		}

		m := linePrefixRegexp.FindStringSubmatch(raw)
		if m != nil {
			n, err := strconv.Atoi(m[1])
			if err != nil {
				continue
			}
			out[n] = strings.TrimRight(m[2], "\r")
			lastLine = n
			inBody = true
			continue
		}

		// Pre-body noise (ClassLoader/Location blocks, CFR comment, package +
		// imports) is discarded — there is no line number to key it under.
		if !inBody {
			continue
		}

		// Continuation: a line in the body without its own /*N*/ prefix
		// belongs to the previous numbered line.
		if lastLine > 0 && trimmed != "" {
			out[lastLine] = out[lastLine] + "\n" + strings.TrimRight(raw, "\r")
		}
	}
	return out
}

// SourceWindow returns the slice of source lines centered on line, with
// `context` lines before and after, and the start line of that slice. Lines
// outside the available range are skipped silently — the returned slice may
// be shorter than 2*context+1 near the start/end of the file.
//
// When `lines` lacks an entry for `line` itself, the function still returns
// the surrounding lines if present (so callers see context even when the
// exact line was elided by the decompiler). startLine is 0 when nothing was
// found.
func SourceWindow(lines map[int]string, line, context int) (window []string, startLine int) {
	window, lineNumbers, startLine := SourceWindowNumbered(lines, line, context)
	_ = lineNumbers
	return window, startLine
}

// SourceWindowNumbered returns source rows with the line number that should be
// shown for each row. It skips missing/blank decompiler rows instead of
// inserting placeholders, and expands unnumbered continuation text so every
// visible source row has a gutter number.
func SourceWindowNumbered(lines map[int]string, line, context int) (window []string, lineNumbers []int, startLine int) {
	if line <= 0 || len(lines) == 0 || context < 0 {
		return nil, nil, 0
	}
	from := line - context
	if from < 1 {
		from = 1
	}
	to := line + context
	for n := from; n <= to; n++ {
		if src, ok := lines[n]; ok {
			if startLine == 0 {
				startLine = n
			}
			for offset, part := range strings.Split(strings.TrimRight(src, "\r"), "\n") {
				if strings.TrimSpace(part) == "" {
					continue
				}
				window = append(window, part)
				lineNumbers = append(lineNumbers, n+offset)
			}
		}
	}
	return window, lineNumbers, startLine
}
