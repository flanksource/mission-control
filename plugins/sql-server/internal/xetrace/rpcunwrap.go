package xetrace

import (
	"regexp"
	"strconv"
	"strings"
)

// UnwrapRPC rewrites a SQL Server RPC invocation into the inner statement it
// is actually executing, with parameter placeholders substituted by their
// literal values. Three shapes are recognized:
//
//  1. sp_prepexec — wrapped in a `declare @p1 int set @p1=N exec sp_prepexec @p1
//     output, N'@P0 type', N'TEMPLATE', v0, v1, … select @p1` scaffold.
//  2. sp_executesql — `[exec] sp_executesql N'TEMPLATE', N'@P0 type', v0, v1, …`.
//  3. Positional CALL — `{call proc(?, ?, ?)}` or `call proc(?, ?, ?)` with
//     values supplied as additional ordered args (we don't see those at this
//     layer, so positional CALL is returned untouched with `?` placeholders).
//
// When no rewrite applies the original input is returned verbatim along with
// ok=false, so callers can decide whether to fall back to raw rendering.
func UnwrapRPC(raw string) (unwrapped string, ok bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return raw, false
	}

	if stripped, didStrip := stripPrepexecScaffold(trimmed); didStrip {
		if inner, innerOK := parsePrepexec(stripped); innerOK {
			return inner, true
		}
		// Scaffold stripped but body unparseable — surface the body
		// anyway so the user sees the actual call without the wrapping.
		return stripped, true
	}

	if inner, innerOK := parseExecuteSQL(trimmed); innerOK {
		return inner, true
	}

	return raw, false
}

// prepexecScaffoldRe matches the `declare @pN int set @pN=M exec sp_prepexec
// @pN output,` prefix that precedes every sp_prepexec call. Case-insensitive
// (SQL Server emits both `declare` and `DECLARE`).
var prepexecScaffoldRe = regexp.MustCompile(`(?is)^\s*declare\s+@p\d+\s+int\s+set\s+@p\d+\s*=\s*\d+\s+exec(?:ute)?\s+sp_prepexec\s+@p\d+\s+output\s*,\s*`)

// prepexecTrailRe matches the trailing `select @pN` that sp_prepexec batches
// always append so the caller can read the handle.
var prepexecTrailRe = regexp.MustCompile(`(?is)\s+select\s+@p\d+\s*$`)

// stripPrepexecScaffold removes the declare/set/exec prefix and the trailing
// select @pN from a sp_prepexec batch. Returns the trimmed middle plus ok.
func stripPrepexecScaffold(s string) (string, bool) {
	loc := prepexecScaffoldRe.FindStringIndex(s)
	if loc == nil {
		return s, false
	}
	body := s[loc[1]:]
	if tail := prepexecTrailRe.FindStringIndex(body); tail != nil {
		body = body[:tail[0]]
	}
	return strings.TrimSpace(body), true
}

// parsePrepexec parses a scaffold-stripped sp_prepexec body of the form:
//
//	N'@P0 int, @P1 nvarchar(10)', N'TEMPLATE', value0, value1, ...
//
// SQL Server also accepts NULL for "no parameter declarations":
//
//	NULL, N'TEMPLATE', value0, ...
//
// The first arg is the param decl (NULL or string), the second is the SQL
// template, and the rest are positional values matched to @P0/@P1/…
// Whitespace around commas is tolerated.
func parsePrepexec(body string) (string, bool) {
	args, ok := splitTopLevelArgs(body)
	if !ok || len(args) < 2 {
		return "", false
	}
	paramDecl := ""
	if !strings.EqualFold(strings.TrimSpace(args[0]), "NULL") {
		decl, declOK := asStringLiteral(args[0])
		if !declOK {
			return "", false
		}
		paramDecl = decl
	}
	template, ok := asStringLiteral(args[1])
	if !ok {
		return "", false
	}
	values := args[2:]
	return substituteParams(template, paramDecl, values), true
}

// executeSQLRe matches `[exec[ute]] sp_executesql <body>` at the start of a
// statement. Unlike sp_prepexec there is no wrapping declare/select.
var executeSQLRe = regexp.MustCompile(`(?is)^\s*(?:exec(?:ute)?\s+)?sp_executesql\s+`)

func parseExecuteSQL(s string) (string, bool) {
	loc := executeSQLRe.FindStringIndex(s)
	if loc == nil {
		return "", false
	}
	body := strings.TrimSpace(s[loc[1]:])
	args, ok := splitTopLevelArgs(body)
	if !ok || len(args) < 1 {
		return "", false
	}
	template, ok := asStringLiteral(args[0])
	if !ok {
		return "", false
	}
	// Param declaration is optional: `sp_executesql N'SELECT 1'` is valid.
	paramDecl := ""
	var values []string
	if len(args) >= 2 {
		if decl, declOK := asStringLiteral(args[1]); declOK {
			paramDecl = decl
			values = args[2:]
		} else {
			// No decl string — second arg onward are the values.
			values = args[1:]
		}
	}
	return substituteParams(template, paramDecl, values), true
}

// splitTopLevelArgs splits a comma-separated argument list respecting string
// literals (including N'...' with doubled ” escapes) and parenthesis depth.
// Whitespace is preserved inside args but trimmed at their edges.
func splitTopLevelArgs(s string) ([]string, bool) {
	var args []string
	var cur strings.Builder
	depth := 0
	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case c == '\'' || (c == 'N' && i+1 < len(s) && s[i+1] == '\''):
			// Consume a full string literal, including opening N if present.
			if c == 'N' {
				cur.WriteByte('N')
				i++
				c = s[i]
			}
			cur.WriteByte('\'')
			i++
			for i < len(s) {
				if s[i] == '\'' {
					if i+1 < len(s) && s[i+1] == '\'' {
						cur.WriteString("''")
						i += 2
						continue
					}
					cur.WriteByte('\'')
					i++
					break
				}
				cur.WriteByte(s[i])
				i++
			}
		case c == '(':
			depth++
			cur.WriteByte(c)
			i++
		case c == ')':
			depth--
			cur.WriteByte(c)
			i++
		case c == ',' && depth == 0:
			args = append(args, strings.TrimSpace(cur.String()))
			cur.Reset()
			i++
		default:
			cur.WriteByte(c)
			i++
		}
	}
	if cur.Len() > 0 {
		args = append(args, strings.TrimSpace(cur.String()))
	}
	return args, true
}

// asStringLiteral unquotes a (possibly N-prefixed) SQL string literal into
// its textual value, collapsing ” escape pairs into single quotes. Returns
// ok=false when the input is not a well-formed literal.
func asStringLiteral(s string) (string, bool) {
	if len(s) >= 2 && s[0] == 'N' {
		s = s[1:]
	}
	if len(s) < 2 || s[0] != '\'' || s[len(s)-1] != '\'' {
		return "", false
	}
	inner := s[1 : len(s)-1]
	return strings.ReplaceAll(inner, "''", "'"), true
}

// paramNameRe captures `@P0`, `@P12`, etc. from a declaration string.
var paramNameRe = regexp.MustCompile(`@P\d+`)

// substituteParams returns `template` with @P0/@P1/... replaced by the
// corresponding entries in `values`. Extra values beyond the declared
// parameter count are ignored; missing values leave the placeholder in
// place so the output still parses as SQL.
func substituteParams(template, paramDecl string, values []string) string {
	names := paramNameRe.FindAllString(paramDecl, -1)
	// If the decl didn't list names, infer them from the template itself
	// so sp_executesql without an explicit decl still substitutes.
	if len(names) == 0 {
		names = paramNameRe.FindAllString(template, -1)
		names = dedupePreserveOrder(names)
	}

	out := template
	for i, name := range names {
		if i >= len(values) {
			break
		}
		literal := formatArgForDisplay(values[i])
		// Replace only whole-word occurrences: `@P0` should not match
		// inside `@P01`. Use a bounded regexp per name.
		re := regexp.MustCompile(regexp.QuoteMeta(name) + `\b`)
		out = re.ReplaceAllLiteralString(out, literal)
	}
	return strings.TrimSpace(out)
}

func dedupePreserveOrder(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := in[:0]
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// formatArgForDisplay normalizes a raw argument token into a form suitable
// for inlining into a SQL statement. String literals keep their quotes,
// numbers render bare, NULL renders as-is, and anything else is returned
// verbatim.
func formatArgForDisplay(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "NULL"
	}
	if strings.EqualFold(raw, "NULL") {
		return "NULL"
	}
	if val, ok := asStringLiteral(raw); ok {
		// Re-quote with single quotes so the inlined SQL is syntactically
		// valid; drop the N-prefix since the literal is already a string.
		return "'" + strings.ReplaceAll(val, "'", "''") + "'"
	}
	if _, err := strconv.ParseFloat(raw, 64); err == nil {
		return raw
	}
	return raw
}
