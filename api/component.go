package api

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/flanksource/incident-commander/db/types"
	"github.com/google/uuid"
)

type Component struct {
	ID            uuid.UUID           `json:"id,omitempty" gorm:"default:generate_ulid()"` //nolint
	Name          string              `json:"name,omitempty"`
	Text          string              `json:"text,omitempty"`
	Schedule      string              `json:"schedule,omitempty"`
	TopologyType  string              `json:"topology_type,omitempty"`
	Namespace     string              `json:"namespace,omitempty"`
	Labels        types.JSONStringMap `json:"labels,omitempty"`
	Owner         string              `json:"owner,omitempty"`
	Status        string              `json:"status,omitempty"`
	StatusReason  string              `json:"statusReason,omitempty"`
	Path          string              `json:"path,omitempty"`
	Order         int                 `json:"order,omitempty"  gorm:"-"`
	Type          string              `json:"type,omitempty"`
	Lifecycle     string              `json:"lifecycle,omitempty"`
	Properties    types.JSON          `json:"properties,omitempty"`
	CreatedAt     time.Time           `json:"created_at,omitempty" time_format:"postgres_timestamp"`
	UpdatedAt     time.Time           `json:"updated_at,omitempty" time_format:"postgres_timestamp"`
	DeletedAt     *time.Time          `json:"deleted_at,omitempty" time_format:"postgres_timestamp" swaggerignore:"true"`
	IsLeaf        bool                `json:"is_leaf"`
	CostPerMinute float64             `gorm:"column:cost_per_minute"`
	Cost1d        float64             `gorm:"column:cost_total_1d"`
	Cost7d        float64             `gorm:"column:cost_total_7d"`
	Cost30d       float64             `gorm:"column:cost_total_30d"`
}

func (c Component) AsMap() map[string]any {
	m := make(map[string]any)
	b, _ := json.Marshal(&c)
	_ = json.Unmarshal(b, &m)
	return m
}

type ComponentSelector struct {
	Name      string            `json:"name,omitempty"`
	Namespace string            `json:"namespace,omitempty"`
	Selector  string            `json:"selector,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
	Types     Items             `json:"types,omitempty"`
}

func (c ComponentSelector) String() string {
	s := ""
	if c.Name != "" {
		s += " name=" + c.Name
	}
	if c.Namespace != "" {
		s += " namespace=" + c.Namespace
	}
	if c.Selector != "" {
		s += " " + c.Selector
	}
	if c.Labels != nil {
		s += fmt.Sprintf(" labels=%s", c.Labels)
	}
	if len(c.Types) > 0 {
		s += " types=" + c.Types.String()
	}
	return strings.TrimSpace(s)

}
