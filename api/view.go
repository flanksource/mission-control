package api

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/flanksource/clicky/api"
	"github.com/flanksource/duty/types"
	"github.com/flanksource/duty/view"
	"github.com/google/uuid"
)

// DisplayTable defines table specific display options
// +kubebuilder:object:generate=true
type DisplayTable struct {
	// Sort defines the default sort column (single field). Prefix with "-" for descending.
	// +kubebuilder:validation:Pattern=`^[+-]?[A-Za-z0-9_.]+$`
	Sort string `json:"sort,omitempty" yaml:"sort,omitempty"`

	// Size defines the default page size for the table
	Size int `json:"size,omitempty" yaml:"size,omitempty"`
}

// DisplayCard defines card layout configuration at the view level
type DisplayCard struct {
	// Columns defines the number of columns for the card body layout
	Columns int `json:"columns,omitempty" yaml:"columns,omitempty"`

	// Default indicates if this is the default card layout
	Default bool `json:"default,omitempty" yaml:"default,omitempty"`
}

// ViewListItem is the response to listing views for a config selector.
type ViewListItem struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Namespace string    `json:"namespace"`
	Title     string    `json:"title,omitempty"`
	Icon      string    `json:"icon,omitempty"`
}

// ColumnFilterOptions holds filter options for a column.
// For regular columns, List contains distinct values.
// For labels columns, Labels contains keys mapped to their possible values.
type ColumnFilterOptions struct {
	List   []string            `json:"list,omitempty"`
	Labels map[string][]string `json:"labels,omitempty"`
}

type ViewRef struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

// +kubebuilder:object:generate=true
// ViewSection is a section rendered in the application view
type ViewSection struct {
	Title   string   `json:"title"`
	Icon    string   `json:"icon,omitempty"`
	ViewRef *ViewRef `json:"viewRef,omitempty"`
	UIRef   *UIRef   `json:"uiRef,omitempty"`
}

// +kubebuilder:object:generate=true
// UIRef references a native Flanksource UI component (changes or configs)
type UIRef struct {
	Changes    *ChangesUIFilters    `json:"changes,omitempty"`
	Configs    *ConfigsUIFilters    `json:"configs,omitempty"`
	Access     *AccessUIFilters     `json:"access,omitempty"`
	AccessLogs *AccessLogsUIFilters `json:"accessLogs,omitempty"`
}

// +kubebuilder:object:generate=true
// ChangesUIFilters defines filters for the native Changes UI component.
// Uses standard formats: Kubernetes-style tag selectors, comma-separated values.
// Frontend is responsible for translating to its internal format.
type ChangesUIFilters struct {
	ConfigTypes string `json:"configTypes,omitempty"` // e.g. "AWS::Account,-Kubernetes::Pod"
	ChangeType  string `json:"changeType,omitempty"`  // e.g. "diff,-BackOff"
	Severity    string `json:"severity,omitempty"`    // e.g. "high" (single value)
	From        string `json:"from,omitempty"`        // e.g. "24h", "7d"
	To          string `json:"to,omitempty"`
	Tags        string `json:"tags,omitempty"`      // e.g. "env=production,!env=staging" (Kubernetes style)
	Source      string `json:"source,omitempty"`    // e.g. "kubernetes,-github"
	Summary     string `json:"summary,omitempty"`   // e.g. "-Failed"
	CreatedBy   string `json:"createdBy,omitempty"` // e.g. "user@example.com,-bot"
}

// +kubebuilder:object:generate=true
// ConfigsUIFilters defines filters for the native Configs UI component.
// Uses standard formats: Kubernetes-style label selectors, comma-separated values.
// Frontend is responsible for translating to its internal format.
type ConfigsUIFilters struct {
	Search     string `json:"search,omitempty"`     // Free text search
	ConfigType string `json:"configType,omitempty"` // e.g. "AWS::RDS::Instance"
	Labels     string `json:"labels,omitempty"`     // e.g. "app=nginx,!team=dev" (Kubernetes style)
	Status     string `json:"status,omitempty"`     // e.g. "Running,-Stopped"
	Health     string `json:"health,omitempty"`     // e.g. "-healthy,warning"
}

// +kubebuilder:object:generate=true
type AccessUIFilters struct {
	Search      string `json:"search,omitempty"`      // maps to ResourceSelector.Search for scoping configs
	ConfigTypes string `json:"configTypes,omitempty"` // e.g. "AWS::IAM::Role,-Kubernetes::Pod"
	Role        string `json:"role,omitempty"`        // e.g. "Owner,-Reader"
	UserType    string `json:"userType,omitempty"`    // e.g. "Member,-Guest"
	Stale       string `json:"stale,omitempty"`       // duration threshold, e.g. "2160h" (90 days)
}

// +kubebuilder:object:generate=true
type AccessLogsUIFilters struct {
	Search      string `json:"search,omitempty"`      // maps to ResourceSelector.Search for scoping configs
	ConfigTypes string `json:"configTypes,omitempty"` // e.g. "AWS::IAM::Role"
	From        string `json:"from,omitempty"`        // e.g. "720h" (30 days)
	To          string `json:"to,omitempty"`
	MFA         string `json:"mfa,omitempty"` // "true" or "false"
}

type SerializedView struct {
	SerializedSection `json:",inline"`
	Namespace         string `json:"namespace,omitempty"`
	Name              string `json:"name"`

	RefreshStatus   string    `json:"refreshStatus,omitempty"`
	RefreshError    string    `json:"refreshError,omitempty"`
	ResponseSource  string    `json:"responseSource,omitempty"`
	LastRefreshedAt time.Time `json:"lastRefreshedAt"`

	RequestFingerprint string              `json:"requestFingerprint,omitempty"`
	Section            []SerializedSection `json:"sections,omitempty"`
}

type SerializedSection struct {
	Title string           `json:"title,omitempty"`
	Icon  string           `json:"icon,omitempty"`
	Data  []map[string]any `json:"data,omitempty"`
	Body  api.Text         `json:"body,omitempty"`
	//Variables used to queries in this section
	Variables map[string]string              `json:"variables,omitempty"`
	Filters   map[string]ColumnFilterOptions `json:"filters,omitempty"`
}

type serializedRow struct {
	cols []string
	data map[string]any
}

func (r serializedRow) Columns() []api.ColumnDef {
	cols := make([]api.ColumnDef, len(r.cols))
	for i, name := range r.cols {
		cols[i] = api.ColumnDef{Name: name}
	}
	return cols
}

func (r serializedRow) Row() map[string]any {
	return r.data
}

func sectionRows(data []map[string]any) []serializedRow {
	seen := map[string]struct{}{}
	var cols []string
	for _, row := range data {
		keys := make([]string, 0, len(row))
		for k := range row {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if _, ok := seen[k]; !ok {
				seen[k] = struct{}{}
				cols = append(cols, k)
			}
		}
	}
	rows := make([]serializedRow, len(data))
	for i, d := range data {
		rows[i] = serializedRow{cols: cols, data: d}
	}
	return rows
}

func (s SerializedSection) Pretty() api.Text {
	t := api.Text{}
	if s.Title != "" {
		t = t.AddText(s.Title, "font-semibold").NewLine()
	}
	if !s.Body.IsEmpty() {
		t = t.Add(s.Body).NewLine()
	}
	if len(s.Variables) > 0 {
		items := make([]api.KeyValuePair, 0, len(s.Variables))
		for k, v := range s.Variables {
			items = append(items, api.KeyValue(k, v))
		}
		t = t.Add(api.DescriptionList{Items: items})
	}
	if len(s.Data) > 0 {
		t = t.Add(api.NewTableFrom(sectionRows(s.Data)))
	}
	return t
}

func (s SerializedSection) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Title string           `json:"title,omitempty"`
		Icon  string           `json:"icon,omitempty"`
		Data  []map[string]any `json:"data,omitempty"`
		Body  api.Text         `json:"body,omitempty"`
	}{Title: s.Title, Icon: s.Icon, Data: s.Data, Body: s.Body})
}

func (s SerializedSection) MarshalYAML() (any, error) {
	return struct {
		Title string           `yaml:"title,omitempty"`
		Icon  string           `yaml:"icon,omitempty"`
		Data  []map[string]any `yaml:"data,omitempty"`
		Body  string           `yaml:"body,omitempty"`
	}{Title: s.Title, Icon: s.Icon, Data: s.Data, Body: s.Body.String()}, nil
}

func sectionMap(sections []SerializedSection) map[string]SerializedSection {
	if len(sections) == 0 {
		return nil
	}
	m := make(map[string]SerializedSection, len(sections))
	for _, s := range sections {
		m[strings.ToLower(strings.ReplaceAll(s.Title, " ", "_"))] = s
	}
	return m
}

func (v SerializedView) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Title     string           `json:"title,omitempty"`
		Icon      string           `json:"icon,omitempty"`
		Data      []map[string]any `json:"data,omitempty"`
		Body      api.Text         `json:"body,omitempty"`
		Namespace string           `json:"namespace,omitempty"`
		Name      string           `json:"name"`

		RefreshStatus   string    `json:"refreshStatus,omitempty"`
		RefreshError    string    `json:"refreshError,omitempty"`
		ResponseSource  string    `json:"responseSource,omitempty"`
		LastRefreshedAt time.Time `json:"lastRefreshedAt"`

		RequestFingerprint string                       `json:"requestFingerprint,omitempty"`
		Section            map[string]SerializedSection `json:"sections,omitempty"`
	}{
		Title:              v.Title,
		Icon:               v.Icon,
		Data:               v.Data,
		Body:               v.Body,
		Namespace:          v.Namespace,
		Name:               v.Name,
		RefreshStatus:      v.RefreshStatus,
		RefreshError:       v.RefreshError,
		ResponseSource:     v.ResponseSource,
		LastRefreshedAt:    v.LastRefreshedAt,
		RequestFingerprint: v.RequestFingerprint,
		Section:            sectionMap(v.Section),
	})
}

func (v SerializedView) MarshalYAML() (any, error) {
	return struct {
		Title     string           `yaml:"title,omitempty"`
		Icon      string           `yaml:"icon,omitempty"`
		Data      []map[string]any `yaml:"data,omitempty"`
		Body      string           `yaml:"body,omitempty"`
		Namespace string           `yaml:"namespace,omitempty"`
		Name      string           `yaml:"name"`

		RefreshStatus   string    `yaml:"refreshStatus,omitempty"`
		RefreshError    string    `yaml:"refreshError,omitempty"`
		LastRefreshedAt time.Time `yaml:"lastRefreshedAt"`

		Section map[string]SerializedSection `yaml:"sections,omitempty"`
	}{
		Title:           v.Title,
		Icon:            v.Icon,
		Data:            v.Data,
		Body:            v.Body.String(),
		Namespace:       v.Namespace,
		Name:            v.Name,
		RefreshStatus:   v.RefreshStatus,
		RefreshError:    v.RefreshError,
		LastRefreshedAt: v.LastRefreshedAt,
		Section:         sectionMap(v.Section),
	}, nil
}

func (v SerializedView) Pretty() api.Text {
	title := v.Name
	if v.Namespace != "" {
		title = v.Namespace + "/" + v.Name
	}
	t := api.Text{Content: title, Style: "text-xl font-semibold"}.
		NewLine().
		Add(v.SerializedSection.Pretty()).
		NewLine()
	for _, sub := range v.Section {
		t = t.Add(sub.Pretty()).NewLine()
	}
	return t
}

// ViewResult is the result of a view query
type ViewResult struct {
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
	Title     string `json:"title"`
	Icon      string `json:"icon,omitempty"`

	RefreshStatus   string    `json:"refreshStatus,omitempty"`
	RefreshError    string    `json:"refreshError,omitempty"`
	ResponseSource  string    `json:"responseSource,omitempty"`
	LastRefreshedAt time.Time `json:"lastRefreshedAt"`

	RequestFingerprint string           `json:"requestFingerprint,omitempty"`
	Columns            []view.ColumnDef `json:"columns,omitempty"`

	// Rows is internal execution data (positional []any values) used for
	// persisting to the materialized view table.
	//
	// Never serialized in API responses: frontend fetches table rows via
	// PostgREST using requestFingerprint.
	Rows []view.Row `json:"-"`

	Panels []PanelResult `json:"panels,omitempty"`

	Variables []ViewVariableWithOptions `json:"variables,omitempty"`

	// List of all possible values for each column where filter is enabled.
	ColumnOptions map[string]ColumnFilterOptions `json:"columnOptions,omitempty"`

	// Card defines the default card layout configuration
	Card *DisplayCard `json:"card,omitempty"`

	// Table defines the default table display configuration
	Table *DisplayTable `json:"table,omitempty"`

	Sections []ViewSection `json:"sections,omitempty"`

	// SectionResults holds resolved viewRef results inline
	SectionResults []ViewSectionResult `json:"sectionResults,omitempty"`
}

type ViewSectionResult struct {
	Title string      `json:"title"`
	Icon  string      `json:"icon,omitempty"`
	Error string      `json:"error,omitempty"`
	View  *ViewResult `json:"view,omitempty"`
}

type MultiViewResult struct {
	Views []ViewResult `json:"views"`
}

func (r *ViewResult) Serialized() SerializedView {
	sv := SerializedView{
		Name:      r.Name,
		Namespace: r.Namespace,
		SerializedSection: SerializedSection{
			Title: r.Title,
			Icon:  r.Icon,
			Data:  r.rowsToData(),
		},
		RefreshStatus:   r.RefreshStatus,
		RefreshError:    r.RefreshError,
		LastRefreshedAt: r.LastRefreshedAt,
	}
	for _, sr := range r.SectionResults {
		if sr.View != nil {
			sub := sr.View.Serialized()
			sv.Section = append(sv.Section, SerializedSection{
				Title: sr.Title,
				Icon:  sr.Icon,
				Data:  sub.SerializedSection.Data,
				Body:  sub.SerializedSection.Body,
			})
		} else if sr.Error != "" {
			sv.Section = append(sv.Section, SerializedSection{
				Title: sr.Title,
				Icon:  sr.Icon,
				Body:  api.Text{}.Add(api.Badge("Error: "+sr.Error, "text-red-700", "bg-red-100")),
			})
		}
	}
	return sv
}

func (r *ViewResult) rowsToData() []map[string]any {
	if len(r.Rows) == 0 {
		return nil
	}
	data := make([]map[string]any, len(r.Rows))
	for i, row := range r.Rows {
		m := make(map[string]any, len(r.Columns))
		for j, col := range r.Columns {
			if col.Hidden || col.Type == "row_attributes" || col.Type == "grants" {
				continue
			}
			if j < len(row) {
				m[col.Name] = row[j]
			}
		}
		data[i] = m
	}
	return data
}

const (
	ViewRefreshStatusCache = "cache"
	ViewRefreshStatusFresh = "fresh"
	ViewRefreshStatusError = "error"

	ViewResponseSourceCache = "cache"
	ViewResponseSourceFresh = "fresh"
)

// +kubebuilder:object:generate=true
// +kubebuilder:validation:XValidation:rule="(has(self.values) && !has(self.valueFrom)) || (!has(self.values) && has(self.valueFrom))",message="exactly one of values or valueFrom is required"
//
// A variable represents a dynamic parameter that can be substituted into data queries.
// Modifying the variable's value will update all elements that reference it.
// Variables appear as interactive controls (dropdown menus or text inputs) at the top of the view interface.
type ViewVariable struct {
	// Key is the unique identifier of the variable.
	// The variable's value is accessible in the dataquery templates as $(var.<key>)
	Key string `json:"key" yaml:"key"`

	// Label is the human-readable name of the variable.
	Label string `json:"label" yaml:"label"`

	// Values is the list of values that the variable can take.
	Values []string `json:"values,omitempty" yaml:"values,omitempty"`

	// ValueFrom is the source of the variable's values.
	ValueFrom *ViewVariableValueFrom `json:"valueFrom,omitempty" yaml:"valueFrom,omitempty"`

	// Default is the default value of the variable.
	Default string `json:"default,omitempty" yaml:"default,omitempty"`

	// Variables this variable depends on - must be resolved before this variable can be populated
	DependsOn []string `json:"dependsOn,omitempty" yaml:"dependsOn,omitempty"`
}

// +kubebuilder:object:generate=true
type ViewVariableValueFrom struct {
	Config types.ResourceSelector `json:"config" yaml:"config"`

	// Label is a cel expression for the dropdown labels rendered for config-derived variables.
	Label types.CelExpression `json:"label,omitempty" yaml:"label,omitempty"`

	// Value is a cel expression for the dropdown values rendered for config-derived variables.
	Value types.CelExpression `json:"value,omitempty" yaml:"value,omitempty"`
}

type ViewVariableOption struct {
	Label string `json:"label" yaml:"label"`
	Value string `json:"value" yaml:"value"`
}

type ViewVariableWithOptions struct {
	ViewVariable `json:",inline" yaml:",inline"`
	Options      []string             `json:"options" yaml:"options"`
	OptionItems  []ViewVariableOption `json:"optionItems,omitempty" yaml:"optionItems,omitempty"`
}
