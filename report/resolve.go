// Resolves the facet rendering server from report options and properties.
// Rendering uses the resolved server, falling back to the local facet binary.
package report

import (
	"net/url"
	"strings"

	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/connection"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"

	v1 "github.com/flanksource/incident-commander/api/v1"
)

const (
	// PropertyConnection names the facet connection used when report options
	// don't specify one.
	PropertyConnection = "facet.connection"

	// PropertyURL is the facet server URL used when report options don't
	// specify one and no connection is configured.
	PropertyURL = "facet.url"
)

// Server is a facet rendering server. The zero value means no server is
// configured and rendering falls back to the local facet binary.
type Server struct {
	BaseURL      string
	Token        string
	TimestampURL string
}

func (s Server) Configured() bool { return s.BaseURL != "" }

// requireSecureToken refuses to send the facet API key over a plaintext
// connection, where it would be readable in transit.
func (s Server) requireSecureToken() error {
	if s.Token == "" {
		return nil
	}

	parsed, err := url.Parse(s.BaseURL)
	if err != nil {
		return dutyAPI.Errorf(dutyAPI.EINVALID, "invalid facet server url %q: %v", s.BaseURL, err)
	}
	if !strings.EqualFold(parsed.Scheme, "https") {
		return dutyAPI.Errorf(dutyAPI.EINVALID, "refusing to send the facet api key to %q over %s: use https", s.BaseURL, parsed.Scheme)
	}
	return nil
}

// ResolveServer resolves the facet rendering server, preferring the report
// options and falling back to the facet.url and facet.connection properties.
func ResolveServer(ctx context.Context, opts *v1.FacetOptions) (Server, error) {
	var timestampURL string
	if opts != nil {
		timestampURL = opts.TimestampURL
	}

	if opts != nil && (opts.URL != "" || opts.Connection != "") {
		server := Server{TimestampURL: timestampURL}
		if opts.Connection != "" {
			resolved, err := resolveConnection(ctx, opts.Connection, timestampURL)
			if err != nil {
				return Server{}, err
			}
			server = resolved
		}
		if opts.URL != "" {
			server.BaseURL = opts.URL
		}
		return server, nil
	}

	if url := ctx.Properties().String(PropertyURL, ""); url != "" {
		return Server{BaseURL: url, TimestampURL: timestampURL}, nil
	}

	if name := ctx.Properties().String(PropertyConnection, ""); name != "" {
		return resolveConnection(ctx, name, timestampURL)
	}

	return Server{TimestampURL: timestampURL}, nil
}

func resolveConnection(ctx context.Context, name, timestampURL string) (Server, error) {
	conn, err := connection.Get(ctx, name)
	if err != nil {
		return Server{}, ctx.Oops().Wrapf(err, "failed to get facet connection %q", name)
	}
	if conn == nil {
		return Server{}, dutyAPI.Errorf(dutyAPI.ENOTFOUND, "facet connection %q not found", name)
	}
	if conn.Type != models.ConnectionTypeFacet {
		return Server{}, dutyAPI.Errorf(dutyAPI.EINVALID, "connection %q is type %q, expected %q", name, conn.Type, models.ConnectionTypeFacet)
	}

	server := Server{
		BaseURL:      conn.URL,
		Token:        conn.Password,
		TimestampURL: timestampURL,
	}
	if server.TimestampURL == "" {
		server.TimestampURL = conn.Properties["timestampUrl"]
	}
	return server, nil
}

// Render resolves the facet server from opts and renders data to the given
// format. An empty srcDir renders the embedded report files; otherwise the
// report scaffold in srcDir is used.
func Render(ctx context.Context, data any, format, entryFile, srcDir string, opts *v1.FacetOptions) (*RenderResult, error) {
	server, err := ResolveServer(ctx, opts)
	if err != nil {
		return nil, err
	}
	return RenderWith(ctx, data, format, entryFile, srcDir, server)
}

// RenderWith renders data using an already resolved server, falling back to the
// local facet binary when the server is not configured. Callers that report
// progress resolve the server first so they can name it before rendering.
func RenderWith(ctx context.Context, data any, format, entryFile, srcDir string, server Server) (*RenderResult, error) {
	if srcDir == "" {
		if dir, override := ResolveSource(); dir != "" {
			srcDir = dir
			if override != "" {
				entryFile = override
			}
		}
	}

	if server.Configured() {
		if err := server.requireSecureToken(); err != nil {
			return nil, err
		}

		httpOpts := RenderHTTPOptions{TimestampURL: server.TimestampURL}

		var rendered []byte
		var err error
		if srcDir == "" {
			rendered, err = RenderHTTP(ctx, server.BaseURL, server.Token, data, format, entryFile, httpOpts)
		} else {
			rendered, err = RenderHTTPFromDir(ctx, server.BaseURL, server.Token, data, format, srcDir, entryFile, httpOpts)
		}
		if err != nil {
			return nil, err
		}
		return &RenderResult{Data: rendered, SrcDir: srcDir, Entry: entryFile}, nil
	}

	if srcDir == "" {
		return RenderCLI(data, format, entryFile)
	}
	return RenderCLIFromDir(data, format, srcDir, entryFile)
}
