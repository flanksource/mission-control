package api

import (
	"fmt"

	"github.com/flanksource/clicky/api"
	"github.com/flanksource/duty/view"
)

func healthBadge(h string) api.Textable {
	switch h {
	case "healthy":
		return api.Badge(h, "text-green-700", "bg-green-100")
	case "warning":
		return api.Badge(h, "text-yellow-700", "bg-yellow-100")
	case "unhealthy":
		return api.Badge(h, "text-red-700", "bg-red-100")
	default:
		return api.Badge(h, "text-gray-600", "bg-gray-100")
	}
}

func severityBadge(s string) api.Textable {
	switch s {
	case "critical":
		return api.Badge(s, "text-red-700", "bg-red-100")
	case "high":
		return api.Badge(s, "text-orange-700", "bg-orange-100")
	case "medium":
		return api.Badge(s, "text-yellow-700", "bg-yellow-100")
	case "low":
		return api.Badge(s, "text-blue-700", "bg-blue-100")
	default:
		return api.Badge(s, "text-gray-600", "bg-gray-100")
	}
}

func statusBadge(s string) api.Textable {
	return api.Badge(s, "text-gray-700", "bg-gray-100")
}

// Pretty returns a key-value panel for the application detail fields.
func (d ApplicationDetail) Pretty() api.Text {
	dl := api.DescriptionList{
		Items: []api.KeyValuePair{
			api.KeyValue("Name", d.Name),
			api.KeyValue("Type", d.Type),
			api.KeyValue("Namespace", d.Namespace),
			api.KeyValue("Created", d.CreatedAt),
		},
	}
	if d.Description != "" {
		dl.Items = append(dl.Items, api.KeyValue("Description", d.Description))
	}
	t := api.Text{}.Add(dl)
	if len(d.Properties) > 0 {
		t = t.NewLine().Add(propertiesTable(d.Properties))
	}
	return t
}

func propertiesTable(props []Property) api.TextTable {
	headers := api.TextList{
		api.Text{Content: "Name"},
		api.Text{Content: "Value"},
	}
	rows := make([]api.TableRow, 0, len(props))
	for _, p := range props {
		label := p.Label
		if label == "" {
			label = p.Name
		}
		value := p.Text
		if value == "" && p.Value != nil {
			value = fmt.Sprintf("%d", *p.Value)
			if p.Unit != "" {
				value += " " + p.Unit
			}
		}
		rows = append(rows, api.TableRow{
			"Name":  api.NewTypedValue(api.Text{Content: label}),
			"Value": api.NewTypedValue(api.Text{Content: value}),
		})
	}
	return api.TextTable{Headers: headers, Rows: rows}
}

// Pretty returns a single-row text with role badge and optional lastAccessReview.
func (u UserAndRole) Pretty() api.Text {
	t := api.Text{Content: u.Name}.
		Add(api.Text{Content: " <" + u.Email + ">", Style: "text-gray-500"}).
		Add(api.Text{Content: " "}).
		Add(api.Badge(u.Role, "text-blue-700", "bg-blue-100"))
	if u.LastAccessReview != nil {
		t = t.AddText(" reviewed: ").Append(*u.LastAccessReview, "text-gray-500")
	}
	return t
}

// Pretty returns a table of users plus auth-methods sub-section.
func (ac ApplicationAccessControl) Pretty() api.Text {
	headers := api.TextList{
		api.Text{Content: "Name"},
		api.Text{Content: "Email"},
		api.Text{Content: "Role"},
		api.Text{Content: "Auth Type"},
		api.Text{Content: "Last Login"},
		api.Text{Content: "Last Review"},
	}
	rows := make([]api.TableRow, 0, len(ac.Users))
	for _, u := range ac.Users {
		row := api.TableRow{
			"Name":        api.NewTypedValue(api.Text{Content: u.Name}),
			"Email":       api.NewTypedValue(api.Text{Content: u.Email}),
			"Role":        api.NewTypedValue(api.Badge(u.Role, "text-blue-700", "bg-blue-100")),
			"Auth Type":   api.NewTypedValue(api.Text{Content: u.AuthType}),
			"Last Login":  api.NewTypedValue(api.Human(u.LastLogin)),
			"Last Review": api.NewTypedValue(api.Human(u.LastAccessReview)),
		}
		rows = append(rows, row)
	}
	t := api.Text{}.Add(api.TextTable{Headers: headers, Rows: rows})
	if len(ac.Authentication) > 0 {
		t = t.NewLine().AddText("Authentication Methods", "font-semibold").NewLine()
		for _, auth := range ac.Authentication {
			t = t.Add(auth.Pretty()).NewLine()
		}
	}
	return t
}

// Pretty returns a text describing the auth method.
func (a AuthMethod) Pretty() api.Text {
	t := api.Text{Content: a.Name}.
		Add(api.Badge(a.Type, "text-gray-700", "bg-gray-100"))
	if a.MFA != nil {
		t = t.AddText(" MFA: ").AddText(a.MFA.Type)
		if a.MFA.Enforced == "true" {
			t = t.AddText(" (enforced)", "text-orange-600")
		}
	}
	return t
}

// Pretty returns table-row text with a status badge.
func (b ApplicationBackup) Pretty() api.Text {
	return api.Text{Content: b.Database}.
		AddText(" ").
		Add(api.Human(b.Date)).
		AddText(" ").
		Add(statusBadge(b.Status)).
		AddText(" " + b.Size)
}

// Pretty returns table-row text with a status badge.
func (r ApplicationBackupRestore) Pretty() api.Text {
	return api.Text{Content: r.Database}.
		AddText(" ").
		Add(api.Human(r.Date)).
		AddText(" ").
		Add(statusBadge(r.Status))
}

// Pretty returns key-value text with a severity badge.
func (f ApplicationFinding) Pretty() api.Text {
	return api.Text{}.
		Add(severityBadge(f.Severity)).
		AddText(" ").
		Add(api.Text{Content: f.Title, Style: "font-semibold"}).
		NewLine().
		Add(api.DescriptionList{Items: []api.KeyValuePair{
			api.KeyValue("Type", f.Type),
			api.KeyValue("Status", f.Status),
			api.KeyValue("Last Observed", f.LastObserved),
		}}).
		NewLine().
		AddText(f.Description)
}

// Pretty returns a row text with provider badge.
func (l ApplicationLocation) Pretty() api.Text {
	return api.Text{Content: l.Name}.
		AddText(" (" + l.Account + ") ").
		Add(api.Badge(l.Provider, "text-indigo-700", "bg-indigo-100")).
		Add(api.DescriptionList{Items: []api.KeyValuePair{
			api.KeyValue("Region", l.Region),
			api.KeyValue("Purpose", l.Purpose),
			api.KeyValue("Resources", l.ResourceCount),
		}})
}

// Pretty returns a row with a severity-coloured status badge and formatted date.
func (c ApplicationChange) Pretty() api.Text {
	source := c.CreatedBy
	if source == "" {
		source = c.Source
	}
	return api.Text{}.
		Add(severityBadge(c.Status)).
		AddText(" ").
		Add(api.Human(c.Date)).
		AddText(" ").
		AddText(source, "text-gray-500").
		NewLine().
		AddText(c.Description)
}

// Pretty returns a row with health and status badges and label key-value pairs.
func (ci ApplicationConfigItem) Pretty() api.Text {
	t := api.Text{Content: ci.Name}
	if ci.Type != "" {
		t = t.AddText(" ("+ci.Type+")", "text-gray-500")
	}
	if ci.Health != "" {
		t = t.AddText(" ").Add(healthBadge(ci.Health))
	}
	if ci.Status != "" {
		t = t.AddText(" ").Add(statusBadge(ci.Status))
	}
	if len(ci.Labels) > 0 {
		t = t.NewLine().Add(api.Map(ci.Labels, "badge"))
	}
	return t
}

// Pretty dispatches to the correct sub-renderer based on Type.
func (s ApplicationSection) Pretty() api.Text {
	switch s.Type {
	case SectionTypeView:
		if s.View != nil {
			return s.View.Pretty()
		}
	case SectionTypeChanges:
		if len(s.Changes) > 0 {
			return changesTable(s.Changes)
		}
	case SectionTypeConfigs:
		if len(s.Configs) > 0 {
			return configsTable(s.Configs)
		}
	}
	return api.Text{}
}

// Pretty renders the Columns+Rows matrix as a TextTable.
func (vd ApplicationViewData) Pretty() api.Text {
	if len(vd.Columns) == 0 {
		return api.Text{}
	}
	var visibleCols []view.ColumnDef
	for _, col := range vd.Columns {
		if !col.Hidden {
			visibleCols = append(visibleCols, col)
		}
	}
	if len(visibleCols) == 0 {
		return api.Text{}
	}

	headers := make(api.TextList, len(visibleCols))
	for i, col := range visibleCols {
		headers[i] = api.Text{Content: col.Name}
	}

	colIndex := make(map[string]int, len(vd.Columns))
	for i, col := range vd.Columns {
		colIndex[col.Name] = i
	}

	rows := make([]api.TableRow, 0, len(vd.Rows))
	for _, rawRow := range vd.Rows {
		row := api.TableRow{}
		for _, col := range visibleCols {
			idx, ok := colIndex[col.Name]
			if !ok || idx >= len(rawRow) {
				row[col.Name] = api.NewTypedValue(api.Text{})
				continue
			}
			row[col.Name] = api.NewTypedValue(api.Text{Content: fmt.Sprintf("%v", rawRow[idx])})
		}
		rows = append(rows, row)
	}
	return api.Text{}.Add(api.TextTable{Headers: headers, Rows: rows})
}

func changesTable(changes []ApplicationChange) api.Text {
	headers := api.TextList{
		api.Text{Content: "Age"},
		api.Text{Content: "Type"},
		api.Text{Content: "Severity"},
		api.Text{Content: "Source"},
		api.Text{Content: "Description"},
	}
	rows := make([]api.TableRow, 0, len(changes))
	for _, c := range changes {
		source := c.CreatedBy
		if source == "" {
			source = c.Source
		}
		rows = append(rows, api.TableRow{
			"Age":         api.NewTypedValue(api.Human(c.Date)),
			"Type":        api.NewTypedValue(api.Text{Content: c.ChangeType}),
			"Severity":    api.NewTypedValue(severityBadge(c.Status)),
			"Source":      api.NewTypedValue(api.Text{Content: source}),
			"Description": api.NewTypedValue(api.Text{Content: c.Description}),
		})
	}
	return api.Text{}.Add(api.TextTable{Headers: headers, Rows: rows})
}

func configsTable(configs []ApplicationConfigItem) api.Text {
	headers := api.TextList{
		api.Text{Content: "Name"},
		api.Text{Content: "Type"},
		api.Text{Content: "Health"},
		api.Text{Content: "Status"},
		api.Text{Content: "Labels"},
	}
	rows := make([]api.TableRow, 0, len(configs))
	for _, ci := range configs {
		rows = append(rows, api.TableRow{
			"Name":   api.NewTypedValue(api.Text{Content: ci.Name}),
			"Type":   api.NewTypedValue(api.Text{Content: ci.Type}),
			"Health": api.NewTypedValue(healthBadge(ci.Health)),
			"Status": api.NewTypedValue(statusBadge(ci.Status)),
			"Labels": api.NewTypedValue(api.Map(ci.Labels, "badge")),
		})
	}
	return api.Text{}.Add(api.TextTable{Headers: headers, Rows: rows})
}
