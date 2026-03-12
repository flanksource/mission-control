package connection

import (
	"strings"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	"github.com/flanksource/commons/har"
)

func (r TestResult) Pretty() api.Text {
	t := api.Text{}

	if len(r.Payload) > 0 {
		t = t.Add(clicky.Map(r.Payload))
	}

	for i, e := range r.Entries {
		if i > 0 || len(r.Payload) > 0 {
			t = t.NewLine()
		}
		t = t.Add(entryPretty(e))
	}

	return t
}

func entryPretty(e har.Entry) api.Text {
	statusStyle := "text-green-600"
	if e.Response.Status >= 400 {
		statusStyle = "text-red-600"
	}

	t := api.Text{}.
		Append(e.Request.Method, "font-bold").Space().
		Append(e.Request.URL, "text-blue-600").
		Append(" → ").
		Appendf("%d %s", e.Response.Status, e.Response.StatusText).Styles(statusStyle).
		Appendf(" (%.0fms)", e.Time).Styles("text-muted")

	if len(e.Request.Headers) > 0 {
		headers := headerMap(e.Request.Headers, nil)
		t = t.NewLine().Append("  Request Headers:", "text-muted").
			NewLine().Add(clicky.Map(headers, "text-sm"))
	}

	if len(e.Response.Headers) > 0 {
		headers := headerMap(e.Response.Headers, isInterestingHeader)
		if len(headers) > 0 {
			t = t.NewLine().Append("  Response Headers:", "text-muted").
				NewLine().Add(clicky.Map(headers, "text-sm"))
		}
	}

	if e.Response.Content.Text != "" {
		body := e.Response.Content.Text
		if len(body) > 500 {
			body = body[:500] + "..."
		}
		lang := "text"
		if strings.Contains(e.Response.Content.MimeType, "json") {
			lang = "json"
		}
		t = t.NewLine().Add(clicky.Collapsed("Body", api.CodeBlock(lang, body)))
	}

	return t
}

func headerMap(headers []har.Header, filter func(string) bool) map[string]string {
	m := make(map[string]string)
	for _, h := range headers {
		if filter != nil && !filter(h.Name) {
			continue
		}
		m[h.Name] = h.Value
	}
	return m
}

func isInterestingHeader(name string) bool {
	lower := strings.ToLower(name)
	for _, prefix := range []string{"content-type", "x-", "server", "date", "www-authenticate"} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

