package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"text/template"
	"time"

	chttp "github.com/flanksource/commons/http"
	"github.com/flanksource/duty/types"
	"github.com/ohler55/ojg/jp"
)

const (
	// GitHub's statistics endpoints answer 202 on a cold cache and compute the
	// answer in the background. These bound the wait; exhausting it yields no row.
	defaultAcceptedAttempts = 5
	defaultAcceptedWait     = 2 * time.Second

	defaultHTTPTimeout = 30 * time.Second
)

// parseProjectionURLTemplate compiles the per-entry URL. missingkey=error is
// deliberate: a projection naming a field the register does not have is a typo,
// and rendering it as "<no value>" would send a request for a repository called
// "<no value>" and quietly record nothing.
func parseProjectionURLTemplate(raw string) (*template.Template, error) {
	return template.New("url").Option("missingkey=error").Parse(raw)
}

func renderProjectionURL(compiled *template.Template, entry map[string]any) (string, error) {
	var rendered strings.Builder
	if err := compiled.Execute(&rendered, entry); err != nil {
		return "", err
	}
	return rendered.String(), nil
}

// filterProjectionHTTPEntries narrows the fan-out to the entries spec.target.filter
// selects. With no filter every selected entry is in scope, which is what the
// other source kinds assume too.
func filterProjectionHTTPEntries(projection Projection, entries []map[string]any) ([]map[string]any, error) {
	expression := strings.TrimSpace(projection.Spec.Target.Filter)
	if expression == "" {
		return entries, nil
	}

	compiled, err := compileProjectionExpression(expression)
	if err != nil {
		return nil, fmt.Errorf("spec.target.filter: %w", err)
	}

	scoped := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		eligible, err := evalProjectionBool(compiled, projectionActivation(nil, entry, nil, nil))
		if err != nil {
			return nil, fmt.Errorf("spec.target.filter: %w", err)
		}
		if eligible {
			scoped = append(scoped, entry)
		}
	}
	return scoped, nil
}

// verifyProjectionHTTPURL renders the URL against every entry without issuing a
// request, so a template referencing a missing field fails offline.
func verifyProjectionHTTPURL(query ProjectionHTTPQuery, entries []map[string]any) error {
	compiled, err := parseProjectionURLTemplate(query.URL)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if _, err := renderProjectionURL(compiled, entry); err != nil {
			return err
		}
	}
	return nil
}

// resolveProjectionEnvVar reads a static value, expanding $VAR references against
// the environment.
//
// valueFrom is rejected rather than ignored. Resolving a secretKeyRef needs the
// database- and Kubernetes-backed context faro deliberately does not have
// (clientcmd.LocalConnections is nil here), and silently treating an unresolved
// secret as "no credential" would turn an authentication mistake into a register
// full of absent facts.
func resolveProjectionEnvVar(field string, value types.EnvVar) (string, error) {
	if value.ValueFrom != nil {
		return "", fmt.Errorf("%s.valueFrom is not supported by the http source; faro has no secret store — use value with a $VAR reference", field)
	}
	return os.ExpandEnv(value.ValueStatic), nil
}

// projectionHTTPClient builds the client from the declared connection.
//
// Everything the connection can express that faro cannot honour is refused in
// validate() rather than dropped here, so a projection never appears to have
// authenticated when it did not.
func projectionHTTPClient(query ProjectionHTTPQuery) (*chttp.Client, error) {
	client := chttp.NewClient().Timeout(defaultHTTPTimeout)

	if query.TLS.InsecureSkipVerify {
		client = client.InsecureSkipVerify(true)
	}

	bearer, err := resolveProjectionEnvVar("spec.source.query.http.bearer", query.Bearer)
	if err != nil {
		return nil, err
	}
	if bearer != "" {
		client = client.Header("Authorization", "Bearer "+bearer)
	}

	if !query.Authentication.IsEmpty() {
		username, err := resolveProjectionEnvVar("spec.source.query.http.username", query.Username)
		if err != nil {
			return nil, err
		}
		password, err := resolveProjectionEnvVar("spec.source.query.http.password", query.Password)
		if err != nil {
			return nil, err
		}
		client = client.Auth(username, password).Digest(query.Digest).NTLM(query.NTLM).NTLMV2(query.NTLMV2)
	}

	for index, header := range query.Headers {
		value, err := resolveProjectionEnvVar(fmt.Sprintf("spec.source.query.http.headers[%d]", index), header)
		if err != nil {
			return nil, err
		}
		client = client.Header(header.Name, value)
	}

	return client, nil
}

// queryHTTPProjection issues one request per selected target entry.
//
// The rows are the target's own entries enriched with what the API said, which is
// why this reads the target file the apply step is about to rewrite. Reading it
// twice is cheap and keeps the apply engine unaware that a source kind exists
// which depends on its target.
func queryHTTPProjection(projection Projection, query ProjectionHTTPQuery) ([]map[string]any, []ProjectionWarning, error) {
	compiled, err := parseProjectionURLTemplate(query.URL)
	if err != nil {
		return nil, nil, err
	}

	body, err := os.ReadFile(projection.resolvePath(projection.Spec.Target.Path))
	if err != nil {
		return nil, nil, err
	}
	_, _, entries, err := projectionTarget(body, projection.Spec.Target.Select)
	if err != nil {
		return nil, nil, err
	}
	// spec.target.filter scopes which entries a projection owns. Honouring it here
	// as well as at apply time is not just an optimisation: without it the fan-out
	// would issue a request for every entry in the register, including ones this
	// projection has no business reading and whose fields the URL cannot template.
	entries, err = filterProjectionHTTPEntries(projection, entries)
	if err != nil {
		return nil, nil, err
	}

	client, err := projectionHTTPClient(query)
	if err != nil {
		return nil, nil, err
	}

	var selector jp.Expr
	if strings.TrimSpace(query.Select) != "" {
		if selector, err = jp.ParseString(query.Select); err != nil {
			return nil, nil, fmt.Errorf("spec.source.query.http.select: %w", err)
		}
	}

	method := query.Method
	if method == "" {
		method = http.MethodGet
	}

	// Entries keep the order they appear in the register, so successive runs
	// produce the same diff.
	items := make([]map[string]any, 0, len(entries))
	var warnings []ProjectionWarning

	for _, entry := range entries {
		url, err := renderProjectionURL(compiled, entry)
		if err != nil {
			return nil, nil, fmt.Errorf("spec.source.query.http.url: %w", err)
		}

		payload, warning := fetchProjectionRow(client, method, url, selector, query.Accepted)
		if warning != nil {
			warnings = append(warnings, *warning)
			continue
		}

		items = append(items, projectionHTTPRow(entry, url, payload))
	}

	return items, warnings, nil
}

// projectionHTTPRow carries the entry's own scalars alongside the response, so
// spec.match can be written the same way as for every other source kind and
// projectionItemIdentity has a name to report. Nested values are left out: the
// row exists to be matched and mapped, not to duplicate the register.
func projectionHTTPRow(entry map[string]any, url string, payload any) map[string]any {
	row := map[string]any{}
	for key, value := range entry {
		switch value.(type) {
		case string, bool, int, int64, float64, nil:
			row[key] = value
		}
	}
	row["url"] = url
	row["body"] = payload
	return row
}

func fetchProjectionRow(
	client *chttp.Client,
	method, url string,
	selector jp.Expr,
	accepted ProjectionHTTPAccepted,
) (any, *ProjectionWarning) {
	attempts := accepted.Attempts
	if attempts == 0 {
		attempts = defaultAcceptedAttempts
	}
	wait := accepted.Wait
	if wait == 0 {
		wait = defaultAcceptedWait
	}

	ctx := context.Background()
	for attempt := 1; ; attempt++ {
		response, err := client.R(ctx).Do(method, url)
		if err != nil {
			return nil, &ProjectionWarning{Source: url, Message: err.Error(), Count: 1}
		}

		// 202 means the API is computing the answer. Waiting is the whole contract
		// of these endpoints; giving up must leave no row rather than a zero.
		if response.StatusCode == http.StatusAccepted {
			response.Body.Close()
			if attempt >= attempts {
				return nil, &ProjectionWarning{
					Source:  url,
					Message: fmt.Sprintf("still computing after %d attempts (HTTP 202)", attempts),
					Count:   1,
				}
			}
			select {
			case <-ctx.Done():
				return nil, &ProjectionWarning{Source: url, Message: ctx.Err().Error(), Count: 1}
			case <-time.After(wait):
			}
			continue
		}

		if !response.IsOK() {
			status := response.Status
			response.Body.Close()
			return nil, &ProjectionWarning{Source: url, Message: status, Count: 1}
		}

		var payload any
		err = json.NewDecoder(response.Body).Decode(&payload)
		response.Body.Close()
		if err != nil {
			return nil, &ProjectionWarning{Source: url, Message: fmt.Sprintf("decoding response: %v", err), Count: 1}
		}

		if selector != nil {
			selected := selector.Get(payload)
			if len(selected) != 1 {
				return nil, &ProjectionWarning{
					Source:  url,
					Message: fmt.Sprintf("select matched %d values, expected exactly 1", len(selected)),
					Count:   1,
				}
			}
			payload = selected[0]
		}

		return payload, nil
	}
}
