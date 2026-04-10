package views

import (
	"fmt"

	"github.com/flanksource/duty/connection"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/view"

	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/report"
)

type facetViewSectionResult struct {
	Title string            `json:"title"`
	Icon  string            `json:"icon,omitempty"`
	Error string            `json:"error,omitempty"`
	View  *facetViewPayload `json:"view,omitempty"`
}

// facetViewPayload injects Rows into the JSON for facet rendering.
// api.ViewResult keeps Rows as json:"-" for regular API responses.
type facetViewPayload struct {
	*api.ViewResult
	Rows           []view.Row               `json:"rows,omitempty"`
	SectionResults []facetViewSectionResult `json:"sectionResults,omitempty"`
}

type facetMultiViewPayload struct {
	Views []facetViewPayload `json:"views"`
}

func newFacetViewPayload(result *api.ViewResult) *facetViewPayload {
	if result == nil {
		return nil
	}

	payload := &facetViewPayload{
		ViewResult: result,
		Rows:       result.Rows,
	}

	if len(result.SectionResults) > 0 {
		payload.SectionResults = make([]facetViewSectionResult, 0, len(result.SectionResults))
		for _, sr := range result.SectionResults {
			payload.SectionResults = append(payload.SectionResults, facetViewSectionResult{
				Title: sr.Title,
				Icon:  sr.Icon,
				Error: sr.Error,
				View:  newFacetViewPayload(sr.View),
			})
		}
	}

	return payload
}

func newFacetMultiViewPayload(result *api.MultiViewResult) *facetMultiViewPayload {
	if result == nil {
		return nil
	}

	payload := &facetMultiViewPayload{
		Views: make([]facetViewPayload, 0, len(result.Views)),
	}

	for i := range result.Views {
		viewPayload := newFacetViewPayload(&result.Views[i])
		if viewPayload != nil {
			payload.Views = append(payload.Views, *viewPayload)
		}
	}

	return payload
}

func RenderFacetHTML(ctx context.Context, result *api.ViewResult, opts *v1.FacetOptions) ([]byte, error) {
	return renderViewWithFacet(ctx, result, "html", opts)
}

func RenderFacetPDF(ctx context.Context, result *api.ViewResult, opts *v1.FacetOptions) ([]byte, error) {
	return renderViewWithFacet(ctx, result, "pdf", opts)
}

func RenderMultiFacetHTML(ctx context.Context, multi *api.MultiViewResult, opts *v1.FacetOptions) ([]byte, error) {
	return renderFacetWithData(ctx, newFacetMultiViewPayload(multi), "html", opts)
}

func RenderMultiFacetPDF(ctx context.Context, multi *api.MultiViewResult, opts *v1.FacetOptions) ([]byte, error) {
	return renderFacetWithData(ctx, newFacetMultiViewPayload(multi), "pdf", opts)
}

const viewEntryFile = "ViewReport.tsx"

func renderViewWithFacet(ctx context.Context, result *api.ViewResult, format string, opts *v1.FacetOptions) ([]byte, error) {
	if result == nil {
		return nil, fmt.Errorf("view result must not be nil")
	}
	return renderFacetWithData(ctx, newFacetViewPayload(result), format, opts)
}

func renderFacetWithData(ctx context.Context, data any, format string, opts *v1.FacetOptions) ([]byte, error) {
	if data == nil {
		return nil, fmt.Errorf("data must not be nil")
	}

	baseURL, token, _, err := resolveFacetConnection(ctx, opts)
	if err != nil {
		return nil, err
	}

	if baseURL != "" {
		return report.RenderHTTP(ctx, baseURL, token, data, format, viewEntryFile)
	}

	result, err := report.RenderCLI(data, format, viewEntryFile)
	if err != nil {
		return nil, err
	}
	return result.Data, nil
}

func resolveFacetConnection(ctx context.Context, opts *v1.FacetOptions) (baseURL, token, timestampURL string, err error) {
	if opts == nil {
		return "", "", "", nil
	}

	timestampURL = opts.TimestampURL

	if opts.Connection != "" {
		conn, err := connection.Get(ctx, opts.Connection)
		if err != nil {
			return "", "", "", fmt.Errorf("failed to get facet connection: %w", err)
		}
		if conn == nil {
			return "", "", "", fmt.Errorf("facet connection %q not found", opts.Connection)
		}
		if conn.Type != models.ConnectionTypeFacet {
			return "", "", "", fmt.Errorf("connection %q is type %q, expected %q", opts.Connection, conn.Type, models.ConnectionTypeFacet)
		}
		baseURL = conn.URL
		token = conn.Password
		if timestampURL == "" {
			timestampURL = conn.Properties["timestampUrl"]
		}
	}

	if opts.URL != "" {
		baseURL = opts.URL
	}

	return baseURL, token, timestampURL, nil
}
