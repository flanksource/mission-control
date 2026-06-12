package clientcmd

import (
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/flanksource/clicky"
	clickyapi "github.com/flanksource/clicky/api"
)

func printWhoami(w io.Writer, report whoamiReport) {
	out, err := clicky.Format(whoamiText(report), clicky.Flags.FormatOptions)
	if err != nil {
		fmt.Fprintln(w, whoamiText(report).String())
		return
	}
	fmt.Fprint(w, out)
	if !strings.HasSuffix(out, "\n") {
		fmt.Fprintln(w)
	}
}

func whoamiText(report whoamiReport) clickyapi.Text {
	t := clicky.Text("Context", "font-bold text-blue-700")
	if report.Context.Error != "" {
		t = addWhoamiLine(t, "status", clicky.Text(report.Context.Error, "text-red-600"))
	}
	t = addWhoamiLine(t, "config", clicky.Text(report.Context.ConfigPath, "font-mono text-gray-600"))
	if report.Context.Name != "" {
		t = addWhoamiLine(t, "name", clicky.Text(report.Context.Name, "font-bold"))
	}
	if report.Context.SelectedBy != "" {
		t = addWhoamiLine(t, "selected by", clicky.Text(report.Context.SelectedBy, "text-gray-600"))
	}
	if report.Context.Server != "" {
		t = addWhoamiLine(t, "server", clicky.Text(report.Context.Server, "font-mono text-gray-700"))
	}
	if report.Context.DB != "" {
		t = addWhoamiLine(t, "db", clicky.Text(report.Context.DB, "font-mono text-gray-700"))
	}
	if len(report.Context.PropertyKeys) > 0 {
		t = addWhoamiLine(t, "properties", clicky.Text(strings.Join(report.Context.PropertyKeys, ", "), "text-gray-600"))
	}

	t = t.NewLine().NewLine().Append("Database", "font-bold text-blue-700")
	t = addWhoamiLine(t, "status", statusText(report.Database.Status, report.Database.Error))
	if report.Database.URL != "" {
		t = addWhoamiLine(t, "url", clicky.Text(report.Database.URL, "font-mono text-gray-700"))
	}
	if report.Database.Database != "" {
		t = addWhoamiLine(t, "database", clicky.Text(report.Database.Database, "font-bold"))
	}
	if report.Database.User != "" {
		t = addWhoamiLine(t, "user", clicky.Text(report.Database.User, "text-gray-700"))
	}
	if report.Database.Latency != "" {
		t = addWhoamiLine(t, "latency", clicky.Text(report.Database.Latency, "text-gray-600"))
	}

	t = t.NewLine().NewLine().Append("Auth", "font-bold text-blue-700")
	t = addWhoamiLine(t, "status", statusText(report.Auth.Status, report.Auth.Error))
	if report.Auth.Server != "" {
		t = addWhoamiLine(t, "server", clicky.Text(report.Auth.Server, "font-mono text-gray-700"))
	}
	if report.Auth.Endpoint != "" {
		t = addWhoamiLine(t, "endpoint", clicky.Text(report.Auth.Endpoint, "font-mono text-gray-700"))
	}
	if report.Auth.TokenSource != "" {
		t = addWhoamiLine(t, "token source", clicky.Text(report.Auth.TokenSource, "text-gray-700"))
	}
	if report.Auth.TokenExpires != "" {
		value := clicky.Text(report.Auth.TokenExpires, "font-mono text-gray-700")
		if report.Auth.TokenTTL != "" {
			value = value.Append(" ("+report.Auth.TokenTTL+")", "text-gray-500")
		}
		t = addWhoamiLine(t, "token expires", value)
	}
	if report.Auth.RefreshStatus != "" {
		t = addWhoamiLine(t, "refresh", refreshText(report.Auth.RefreshStatus))
	}
	if report.Auth.User != nil {
		t = addWhoamiLine(t, "user", clicky.Text(formatUser(report.Auth.User), "font-bold"))
	}
	if len(report.Auth.Roles) > 0 {
		t = addWhoamiLine(t, "roles", clicky.Text(strings.Join(report.Auth.Roles, ", "), "text-purple-600"))
	}
	if report.Auth.AccessToken != nil {
		value := statusText(report.Auth.AccessToken.Status, report.Auth.AccessToken.Error)
		if report.Auth.AccessToken.AutoRenew {
			value = value.Append(", auto-renew enabled", "text-green-600")
		}
		if report.Auth.AccessToken.Renewed {
			value = value.Append(", renewed", "text-green-600 font-bold")
		}
		if report.Auth.AccessToken.ExpiresAt != "" {
			value = value.Append(", expires "+report.Auth.AccessToken.ExpiresAt, "font-mono text-gray-600")
		}
		t = addWhoamiLine(t, "access token", value)
	}
	return t
}

func addWhoamiLine(t clickyapi.Text, label string, value clickyapi.Textable) clickyapi.Text {
	return t.NewLine().
		Append("  "+label+": ", "text-gray-500").
		Append(value)
}

func statusText(status, err string) clickyapi.Text {
	style := "text-gray-600"
	switch status {
	case "ok", "valid":
		style = "text-green-600 font-bold"
	case "error", "invalid", "expired", "not_found":
		style = "text-red-600 font-bold"
	case "skipped", "unknown":
		style = "text-gray-500"
	}
	return clicky.Text(statusLine(status, err), style)
}

func refreshText(status string) clickyapi.Text {
	style := "text-gray-600"
	switch {
	case status == "refreshed", status == "not needed":
		style = "text-green-600"
	case strings.HasPrefix(status, "failed"), strings.HasPrefix(status, "unavailable"):
		style = "text-red-600"
	case status == "needed":
		style = "text-yellow-600"
	}
	return clicky.Text(status, style)
}

func formatUser(user map[string]any) string {
	parts := []string{}
	for _, key := range []string{"email", "name", "id"} {
		if v, ok := user[key]; ok && fmt.Sprint(v) != "" {
			parts = append(parts, fmt.Sprintf("%s=%v", key, v))
		}
	}
	if len(parts) == 0 {
		return fmt.Sprint(user)
	}
	return strings.Join(parts, " ")
}

func statusLine(status, err string) string {
	if err == "" {
		return status
	}
	return status + " (" + err + ")"
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

func formatTTL(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Until(t).Round(time.Second)
	if d < 0 {
		return "expired " + (-d).String() + " ago"
	}
	return "expires in " + d.String()
}

func redactURL(raw string) string {
	if raw == "" {
		return ""
	}
	if parsed, err := url.Parse(raw); err == nil && parsed.Scheme != "" {
		return parsed.Redacted()
	}
	return raw
}

func sortedMapKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
