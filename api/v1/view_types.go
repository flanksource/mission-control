package v1

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/flanksource/commons/duration"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	"github.com/flanksource/duty/view"
	"github.com/google/uuid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"

	"github.com/flanksource/incident-commander/api"
)

// ViewDisplay defines display properties for the view
type ViewDisplay struct {
	// Ordinal defines the order of the view
	Ordinal int `json:"ordinal,omitempty" yaml:"ordinal,omitempty"`

	// Sidebar indicates if the view should be shown in sidebar
	Sidebar bool `json:"sidebar,omitempty" yaml:"sidebar,omitempty"`

	// Icon to use for the view
	Icon string `json:"icon,omitempty" yaml:"icon,omitempty"`

	// Title of the view to be displayed in the UI
	Title string `json:"title,omitempty" yaml:"title,omitempty"`

	// Card defines the card layout configuration
	Card *api.DisplayCard `json:"card,omitempty" yaml:"card,omitempty"`

	// Plugins tie this view to various pages in the UI.
	//
	// When a view is attached to a config, the view shows up as a tab in the config page.
	Plugins []ViewConfigUIPlugin `json:"plugins,omitempty" yaml:"plugins,omitempty"`
}

type ViewConfigUIPlugin struct {
	// ConfigTab is a selector that determines which config pages should display this view as a tab.
	//
	// When a config matches this selector, the view will appear as a tab on that config's detail page.
	ConfigTab types.ResourceSelector `json:"configTab"`

	// Variables define template expressions to extract data from the config and pass them as variables to the view.
	//
	// The map key is the variable name, and the value is a Go template expression that extracts data from the config.
	// Templates have access to the config object via the `.config` variable.
	//
	// Example:
	//   variables:
	//     namespace: "$(.config.tags.namespace)"
	//     cluster: "$(.config.tags.cluster)"
	Variables map[string]string `json:"variables,omitempty"`
}

// ViewSpec defines the desired state of View
// +kubebuilder:validation:XValidation:rule="size(self.queries) > 0",message="query must be specified"
// +kubebuilder:validation:XValidation:rule="size(self.panels) > 0 || size(self.columns) > 0",message="view spec must have either panels or columns defined"
// +kubebuilder:validation:XValidation:rule="!(has(self.columns)) || size(self.columns) == 0 || self.columns.exists(c, c.primaryKey == true)",message="if columns is specified, at least one column must have primaryKey set to true"
type ViewSpec struct {
	// Description about the view
	Description string `json:"description,omitempty" yaml:"description,omitempty"`

	// Display properties for the view
	Display ViewDisplay `json:"display,omitempty" yaml:"display,omitempty"`

	// Panels for the view
	//+kubebuilder:validation:Optional
	Panels []api.PanelDef `json:"panels,omitempty" yaml:"panels,omitempty"`

	// Columns define the structure of the view
	//+kubebuilder:validation:Optional
	Columns view.ViewColumnDefList `json:"columns" yaml:"columns"`

	// Queries define the queries and mappings to populate the view
	//+kubebuilder:validation:Optional
	Queries map[string]ViewQueryWithColumnDefs `json:"queries" yaml:"queries" template:"true"`

	// Merge defines how to merge/join data from multiple queries
	//+kubebuilder:validation:Optional
	Merge *string `json:"merge,omitempty" yaml:"merge,omitempty"`

	// Mapping defines how to map query results to view columns
	//+kubebuilder:validation:Optional
	//+kubebuilder:validation:Schemaless
	//+kubebuilder:pruning:PreserveUnknownFields
	//+kubebuilder:validation:Type=object
	Mapping map[string]types.CelExpression `json:"mapping,omitempty" yaml:"mapping,omitempty" template:"true"`

	// Cache configuration
	//+kubebuilder:validation:Optional
	Cache ViewCache `json:"cache" yaml:"cache"`

	// Templating parameters for the view data queries.
	//
	// These vars are available as `var.<key>` in the data queries.
	Templating []api.ViewVariable `json:"templating,omitempty" yaml:"templating,omitempty"`
}

type ViewQueryWithColumnDefs struct {
	view.Query `json:",inline" yaml:",inline" template:"true"`

	// Define the column types for the results from the query.
	// It's optional for configs and changes.
	//
	// This information is used to create the Sqlite table for the query.
	// If not provided, the column types will be inferred from the query results.
	// However, if this isn't provided and the query results are empty, the query will result in an error.
	Columns map[string]models.ColumnType `json:"columns,omitempty"`
}

func (t ViewSpec) Validate() error {
	for k, query := range t.Queries {
		if query.IsEmpty() {
			return fmt.Errorf("query %s is empty", k)
		}
	}

	if len(t.Queries) == 0 {
		return fmt.Errorf("view must have at least one query")
	}

	tableOnly := len(t.Columns) > 0 && len(t.Panels) == 0

	if len(t.Queries) > 1 && tableOnly && t.Merge == nil {
		return fmt.Errorf("merge query must be specified when there are multiple queries and no panels")
	}

	if len(t.Columns) > 0 && len(t.Columns.PrimaryKey()) == 0 {
		return fmt.Errorf("view must have at least one primary key column")
	}

	if len(t.Panels) == 0 && len(t.Columns) == 0 {
		return fmt.Errorf("view must have either panels or columns for table")
	}

	return nil
}

// ViewStatus defines the observed state of View
type ViewStatus struct {
	LastRan *metav1.Time `json:"lastRan,omitempty" yaml:"lastRan,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// View is the schema for the Views API
type View struct {
	metav1.TypeMeta   `json:",inline" yaml:",inline"`
	metav1.ObjectMeta `json:"metadata" yaml:"metadata"`

	Spec ViewSpec `json:"spec" yaml:"spec"`

	//+kubebuilder:validation:Optional
	Status ViewStatus `json:"status" yaml:"status"`
}

func (v *View) GetNamespacedName() string {
	return fmt.Sprintf("%s/%s", v.Namespace, v.Name)
}

func (v *View) GetUUID() (uuid.UUID, error) {
	return uuid.Parse(string(v.UID))
}

func (v *View) HasTable() bool {
	return len(v.Spec.Columns) > 0
}

//+kubebuilder:object:root=true

// ViewList contains a list of View
type ViewList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []View `json:"items"`
}

func (v *View) TableName() string {
	cleanNamespace := strings.ReplaceAll(v.Namespace, "-", "_")
	cleanName := strings.ReplaceAll(v.Name, "-", "_")
	return fmt.Sprintf("view_%s_%s", cleanNamespace, cleanName)
}

func (v *View) ToModel() (*models.View, error) {
	specJSON, err := json.Marshal(v.Spec)
	if err != nil {
		return nil, err
	}

	var uid uuid.UUID
	if v.UID != "" {
		uid, err = uuid.Parse(string(v.UID))
		if err != nil {
			return nil, err
		}
	}

	return &models.View{
		ID:        uid,
		Name:      v.Name,
		Namespace: v.Namespace,
		Spec:      specJSON,
		Source:    models.SourceCRD,
	}, nil
}

// CacheExpired checks if the view cache has expired based on lastRan and maxAge
func (v *View) CacheExpired(maxAge time.Duration) bool {
	if v.Status.LastRan == nil {
		return true
	}

	return time.Since(v.Status.LastRan.Time) > maxAge
}

type ViewCache struct {
	// MaxAge is the maximum age of a cache before it's deemed stale.
	// Can be overridden with cache-control headers.
	// Default: 15m
	MaxAge string `json:"maxAge,omitempty" yaml:"maxAge,omitempty"`

	// MinAge is the minimum age of a cache a user can request.
	// Default: 10s
	MinAge string `json:"minAge,omitempty" yaml:"minAge,omitempty"`

	// RefreshTimeout is the duration to wait for a view to process before returning stale data.
	// Default: 5s
	RefreshTimeout string `json:"refreshTimeout,omitempty" yaml:"refreshTimeout,omitempty"`
}

// CacheOptions represents cache control options from headers and spec
type CacheOptions struct {
	MaxAge         time.Duration
	RefreshTimeout time.Duration
}

// GetCacheOptions returns cache options with defaults and header overrides applied
func (v *View) GetCacheOptions(maxAge, refreshTimeout time.Duration) (*CacheOptions, error) {
	opts := &CacheOptions{}

	if maxAge > 0 {
		minAge, err := v.getMinAge()
		if err != nil {
			return nil, fmt.Errorf("failed to parse minAge: %w", err)
		}

		opts.MaxAge = max(maxAge, minAge)
	} else {
		maxAge, err := v.getMaxAge()
		if err != nil {
			return nil, fmt.Errorf("failed to parse maxAge: %w", err)
		}
		opts.MaxAge = maxAge
	}

	if refreshTimeout > 0 {
		opts.RefreshTimeout = refreshTimeout
	} else {
		refreshTimeout, err := v.getRefreshTimeout()
		if err != nil {
			return nil, fmt.Errorf("failed to parse refreshTimeout: %w", err)
		}
		opts.RefreshTimeout = refreshTimeout
	}

	return opts, nil
}

func (v *View) getMaxAge() (time.Duration, error) {
	if v.Spec.Cache.MaxAge == "" {
		return 15 * time.Minute, nil // Default
	}
	d, err := duration.ParseDuration(v.Spec.Cache.MaxAge)
	return time.Duration(d), err
}

func (v *View) getMinAge() (time.Duration, error) {
	if v.Spec.Cache.MinAge == "" {
		return 10 * time.Second, nil // Default
	}
	d, err := duration.ParseDuration(v.Spec.Cache.MinAge)
	return time.Duration(d), err
}

func (v *View) getRefreshTimeout() (time.Duration, error) {
	if v.Spec.Cache.RefreshTimeout == "" {
		return 5 * time.Second, nil // Default
	}
	d, err := duration.ParseDuration(v.Spec.Cache.RefreshTimeout)
	return time.Duration(d), err
}

func ViewFromModel(model *models.View) (*View, error) {
	var spec ViewSpec
	if err := json.Unmarshal(model.Spec, &spec); err != nil {
		return nil, err
	}

	view := View{
		ObjectMeta: metav1.ObjectMeta{
			Name:      model.Name,
			Namespace: model.Namespace,
			UID:       k8stypes.UID(model.ID.String()),
		},
		Spec: spec,
	}

	return &view, nil
}

func init() {
	SchemeBuilder.Register(&View{}, &ViewList{})
}
