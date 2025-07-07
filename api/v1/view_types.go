package v1

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/flanksource/commons/duration"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"

	"github.com/flanksource/incident-commander/api"
)

type ViewMergeStrategy string

const (
	ViewMergeStrategyLeft  ViewMergeStrategy = "left"
	ViewMergeStrategyUnion ViewMergeStrategy = "union"
)

// ViewMergeSpec defines how to merge/join data from multiple queries
type ViewMergeSpec struct {
	// Strategy defines the merge strategy (left join or union)
	// Default: union
	// +kubebuilder:validation:Enum=left;union
	// +kubebuilder:validation:Optional
	Strategy ViewMergeStrategy `json:"strategy" yaml:"strategy"`

	// Order defines the order of queries for joining
	// +kubebuilder:validation:MinItems=1
	Order []string `json:"order" yaml:"order"`
}

// ViewQuery defines a query configuration for populating the view
// +kubebuilder:validation:XValidation:rule="(has(self.configs) && !has(self.changes)) || (!has(self.configs) && has(self.changes))",message="exactly one of configs or changes must be specified"
type ViewQuery struct {
	// PrimaryKey defines the fields used for joining this query with others
	// +kubebuilder:validation:MinItems=1
	PrimaryKey []string `json:"primaryKey" yaml:"primaryKey"`

	// Configs queries config items
	Configs *types.ResourceSelector `json:"configs,omitempty" yaml:"configs,omitempty"`

	// Changes queries config changes
	Changes *types.ResourceSelector `json:"changes,omitempty" yaml:"changes,omitempty"`
}

// ViewSpec defines the desired state of View
// +kubebuilder:validation:XValidation:rule="size(self.panels) > 0 || (size(self.columns) > 0 && size(self.queries) > 0)",message="view spec must have either panels or both columns and queries defined"
type ViewSpec struct {
	// Panels for the view
	//+kubebuilder:validation:Optional
	Panels []api.PanelDef `json:"panels,omitempty" yaml:"panels,omitempty"`

	// Columns define the structure of the view
	//+kubebuilder:validation:Optional
	Columns []api.ViewColumnDef `json:"columns" yaml:"columns"`

	// Queries define the queries and mappings to populate the view
	//+kubebuilder:validation:Optional
	Queries map[string]ViewQuery `json:"queries" yaml:"queries"`

	// Merge defines how to merge/join data from multiple queries
	//+kubebuilder:validation:Optional
	Merge *ViewMergeSpec `json:"merge,omitempty" yaml:"merge,omitempty"`

	// Mapping defines how to map query results to view columns
	//+kubebuilder:validation:Optional
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Type=object
	Mapping map[string]types.CelExpression `json:"mapping,omitempty" yaml:"mapping,omitempty" template:"true"`

	// Cache configuration
	//+kubebuilder:validation:Optional
	Cache ViewCache `json:"cache" yaml:"cache"`
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

// +kubebuilder:object:root=true

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
