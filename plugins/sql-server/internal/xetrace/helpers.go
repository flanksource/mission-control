package xetrace

import "strings"

// collapseWhitespace squashes runs of spaces / tabs / newlines into single
// spaces. Used by the parser + replay paths to normalize captured statement
// text.
func collapseWhitespace(s string) string {
	var b strings.Builder
	prevSpace := false
	for _, r := range s {
		if r == '\n' || r == '\r' || r == '\t' || r == ' ' {
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
			continue
		}
		b.WriteRune(r)
		prevSpace = false
	}
	return b.String()
}
