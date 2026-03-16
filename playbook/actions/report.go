package actions

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/flanksource/artifacts"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/view"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/views"
)

type ReportResult struct {
	Format    string               `json:"format,omitempty"`
	Artifacts []artifacts.Artifact `json:"-"`
}

func (r *ReportResult) GetArtifacts() []artifacts.Artifact { return r.Artifacts }

type Report struct{}

func (r *Report) Run(ctx context.Context, action v1.ReportAction) (*ReportResult, error) {
	if action.View == "" && action.Configs == nil {
		return nil, fmt.Errorf("either view or configs must be specified")
	}

	format := action.Format
	if format == "" {
		format = "json"
	}

	var v *v1.View
	if action.View != "" {
		namespace, name := parseNamespacedName(ctx, action.View)
		var err error
		v, err = db.GetView(ctx, namespace, name)
		if err != nil {
			return nil, fmt.Errorf("failed to get view %s: %w", action.View, err)
		}
		if v == nil {
			return nil, fmt.Errorf("view %s not found", action.View)
		}
	} else {
		v = &v1.View{
			Spec: v1.ViewSpec{
				Queries: map[string]v1.ViewQueryWithColumnDefs{
					"configs": {
						Query: view.Query{
							Configs: action.Configs,
						},
					},
				},
			},
		}
	}

	rendered, err := views.Export(ctx, v, action.Variables, format)
	if err != nil {
		return nil, fmt.Errorf("failed to export view: %w", err)
	}

	return &ReportResult{
		Format: format,
		Artifacts: []artifacts.Artifact{{
			ContentType: formatContentType(format),
			Content:     io.NopCloser(bytes.NewReader(rendered)),
			Path:        "report" + formatExtension(format),
		}},
	}, nil
}

func parseNamespacedName(ctx context.Context, ref string) (string, string) {
	parts := strings.SplitN(ref, "/", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return ctx.GetNamespace(), ref
}

// formatMeta maps known report formats to their MIME content type and file extension.
// When adding new binary export formats (e.g. Excel, images), add an entry here
// to keep formatContentType and formatExtension in sync.
var formatMeta = map[string]struct {
	contentType string
	extension   string
}{
	"pdf":       {"application/pdf", ".pdf"},
	"facet-pdf": {"application/pdf", ".pdf"},
	"html":      {"text/html", ".html"},
	"csv":       {"text/csv", ".csv"},
}

func formatContentType(format string) string {
	if meta, ok := formatMeta[format]; ok {
		return meta.contentType
	}
	return "application/json"
}

func formatExtension(format string) string {
	if meta, ok := formatMeta[format]; ok {
		return meta.extension
	}
	return ".json"
}
