package application

import (
	"encoding/json"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	"github.com/flanksource/duty/context"

	icapi "github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
)

// Render assembles all Pretty() outputs into a top-to-bottom document.
func Render(app *icapi.Application) api.TextList {
	var out api.TextList

	heading := func(title string) api.Text {
		return api.Text{Content: title, Style: "text-xl font-semibold"}
	}

	out = append(out, heading(app.Name))
	out = append(out, app.ApplicationDetail.Pretty())

	if len(app.AccessControl.Users) > 0 || len(app.AccessControl.Authentication) > 0 {
		out = append(out, heading("Access Control"))
		out = append(out, app.AccessControl.Pretty())
	}

	if len(app.Locations) > 0 {
		out = append(out, heading("Locations"))
		for _, loc := range app.Locations {
			out = append(out, loc.Pretty())
		}
	}

	if len(app.Backups) > 0 {
		out = append(out, heading("Backups"))
		for _, b := range app.Backups {
			out = append(out, b.Pretty())
		}
	}

	if len(app.Restores) > 0 {
		out = append(out, heading("Restores"))
		for _, r := range app.Restores {
			out = append(out, r.Pretty())
		}
	}

	if len(app.Findings) > 0 {
		out = append(out, heading("Findings"))
		for _, f := range app.Findings {
			out = append(out, f.Pretty())
		}
	}

	for _, section := range app.Sections {
		content := section.Pretty()
		if content.IsEmpty() {
			continue
		}
		out = append(out, heading(section.Title))
		out = append(out, content)
	}

	return out
}

// RenderHTML renders the application as a full HTML document string.
func RenderHTML(app *icapi.Application) (string, error) {
	return clicky.Format(Render(app), clicky.FormatOptions{Format: "html"})
}

// RenderPDF renders the application as PDF bytes.
func RenderPDF(app *icapi.Application) ([]byte, error) {
	s, err := clicky.Format(Render(app), clicky.FormatOptions{Format: "pdf"})
	if err != nil {
		return nil, err
	}
	return []byte(s), nil
}

// Export builds and renders the application identified by namespace/name as HTML or PDF bytes.
func Export(ctx context.Context, namespace, name, format string) ([]byte, error) {
	application, err := db.FindApplication(ctx, namespace, name)
	if err != nil {
		return nil, ctx.Oops().Errorf("failed to find application %s/%s: %w", namespace, name, err)
	} else if application == nil {
		return nil, ctx.Oops().Errorf("application %s/%s not found", namespace, name)
	}

	app, err := v1.ApplicationFromModel(*application)
	if err != nil {
		return nil, ctx.Oops().Errorf("failed to convert application: %w", err)
	}

	generated, err := buildApplication(ctx, app)
	if err != nil {
		return nil, ctx.Oops().Errorf("failed to build application: %w", err)
	}

	switch format {
	case "pdf":
		return RenderPDF(generated)
	case "html":
		html, err := RenderHTML(generated)
		if err != nil {
			return nil, err
		}
		return []byte(html), nil
	case "facet-html":
		return RenderFacetHTML(generated)
	case "facet-pdf":
		return RenderFacetPDF(generated)
	default:
		out, err := json.MarshalIndent(generated, "", "  ")
		if err != nil {
			return nil, err
		}
		return out, nil
	}
}
