package echo

import (
	"strings"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	"github.com/flanksource/clicky/api/icons"
	clickyfmt "github.com/flanksource/clicky/formatters"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
)

// wantsClicky returns true when the request's Accept header opts into the
// clicky-json payload consumed by the embedded UI.
func wantsClicky(accept string) bool {
	for _, part := range strings.Split(accept, ",") {
		if i := strings.IndexByte(part, ';'); i >= 0 {
			part = part[:i]
		}
		switch strings.TrimSpace(part) {
		case "application/json+clicky", "application/clicky+json":
			return true
		}
	}
	return false
}

// renderSearchClicky formats a SearchResourcesResponse as clicky-json so that
// the embedded UI renders row IDs as clickable link-command nodes pointing at
// the getResource operation.
func renderSearchClicky(resp *query.SearchResourcesResponse) (string, error) {
	rows := make([]resourceRow, 0)
	for _, kind := range [][]query.SelectedResource{
		resp.Configs, resp.Components, resp.Checks, resp.Canaries,
		resp.ConfigChanges, resp.Playbooks, resp.Connections,
	} {
		for _, r := range kind {
			rows = append(rows, resourceRow{SelectedResource: r})
		}
	}

	table := api.NewTableFrom(rows)
	manager := clickyfmt.NewFormatManager()
	return manager.FormatWithOptions(clickyfmt.FormatOptions{Format: "clicky-json"}, table)
}

// resourceRow wraps query.SelectedResource so we can expose it as a
// clicky.TableProvider whose name cell is a LinkCommand targeting the
// getResource detail operation.
type resourceRow struct {
	query.SelectedResource
}

func (r resourceRow) Columns() []api.ColumnDef {
	return []api.ColumnDef{
		{Name: "Icon"},
		{Name: "Name", Style: "font-semibold"},
		{Name: "Type", Style: "text-muted-foreground"},
		{Name: "Namespace", Style: "text-muted-foreground"},
		{Name: "Health"},
		{Name: "Tags"},
		{Name: "Agent", Style: "text-muted-foreground"},
		{Name: "ID", Hidden: true},
	}
}

func (r resourceRow) Row() map[string]any {
	return map[string]any{
		"Icon":      configIcon(r.Icon, r.Type),
		"Name":      clicky.LinkCommand("getResource").WithArgs(r.ID).Append(r.Name, "text-sky-700 underline"),
		"Type":      r.Type,
		"Namespace": r.Namespace,
		"Health":    models.Health(r.Health).Badge(),
		"Tags":      tagsBadges(r.Tags),
		"Agent":     r.Agent,
		"ID":        r.ID,
	}
}

// configIcon picks a single icon name for a resource row. It prefers the
// Icon field populated on the SelectedResource (set for Checks / Canaries /
// Playbooks etc.). For configs, which leave Icon empty, it falls back to the
// raw config Type so the UI can resolve it with ResourceIcon.
func configIcon(icon, configType string) api.Textable {
	name := icon
	if name == "" && configType != "" {
		name = configType
	}
	if name == "" {
		return api.Text{}
	}
	return icons.Icon{Iconify: name}
}

// tagsBadges renders a map of tags as a row of inline pill badges.
// Returns an empty Text when tags is empty so the column stays blank.
func tagsBadges(tags map[string]string) api.Textable {
	if len(tags) == 0 {
		return api.Text{}
	}
	t := clicky.Text("")
	for k, v := range tags {
		t = t.Add(api.Badge(k+"="+v, "bg-gray-100", "text-gray-700", "text-xs")).AddText(" ")
	}
	return t
}
