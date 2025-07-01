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

// ViewQuery defines a query configuration for populating the view
type ViewQuery struct {
	// Selector defines the resource selector for finding matching resources
	Selector types.ResourceSelector `json:"selector" yaml:"selector"`

	// Max number of results to return
	Max int `json:"max,omitempty" yaml:"max,omitempty"`

	// Mapping defines how to map query results to view columns
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Type=object
	Mapping map[string]types.CelExpression `json:"mapping" yaml:"mapping" template:"true"`
}

// ViewQueriesSpec defines the structure for different types of queries
type ViewQueriesSpec struct {
	Configs []ViewQuery `json:"configs,omitempty" yaml:"configs,omitempty"`
	Changes []ViewQuery `json:"changes,omitempty" yaml:"changes,omitempty"`
}

// ViewSpec defines the desired state of View
type ViewSpec struct {
	// Panels for the view
	Panels []api.PanelDef `json:"panels,omitempty" yaml:"panels,omitempty"`

	// Columns define the structure of the view
	Columns []api.ViewColumnDef `json:"columns" yaml:"columns"`

	// Queries define the queries and mappings to populate the view
	Queries ViewQueriesSpec `json:"queries" yaml:"queries"`

	// CacheTTL defines how long to cache the view data (e.g., "1h", "30m")
	CacheTTL string `json:"cacheTTL,omitempty" yaml:"cacheTTL,omitempty"`
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

	Spec   ViewSpec   `json:"spec" yaml:"spec"`
	Status ViewStatus `json:"status" yaml:"status"`
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

// isCacheExpired checks if the view cache has expired based on lastRan and cacheTTL
func (v *View) CacheExpired() bool {
	if v.Status.LastRan == nil {
		return true
	}
	if v.Spec.CacheTTL == "" {
		return false
	}

	duration, err := duration.ParseDuration(v.Spec.CacheTTL)
	if err != nil {
		// The cache TTL must be validated the the operator, so we should never get here.
		return true
	}

	return time.Since(v.Status.LastRan.Time) > time.Duration(duration)
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
