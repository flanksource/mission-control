package pluginpb

import (
	"encoding/json"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	structpb "google.golang.org/protobuf/types/known/structpb"
)

// ToDuty converts the protobuf selector into duty's ResourceSelector.
func (x *ResourceSelector) ToDuty() types.ResourceSelector {
	if x == nil {
		return types.ResourceSelector{}
	}

	return types.ResourceSelector{
		Agent:          x.Agent,
		Scope:          x.Scope,
		Cache:          x.Cache,
		Search:         x.Search,
		Limit:          int(x.Limit),
		IncludeDeleted: x.IncludeDeleted,
		ID:             x.Id,
		Name:           x.Name,
		Namespace:      x.Namespace,
		TagSelector:    x.TagSelector,
		LabelSelector:  x.LabelSelector,
		FieldSelector:  x.FieldSelector,
		Health:         types.MatchExpression(x.Health),
		Types:          types.Items(x.Types),
		Statuses:       types.Items(x.Statuses),
	}
}

func FromConfigItem(item models.ConfigItem) (*ConfigItem, error) {
	out := &ConfigItem{
		Id: item.ID.String(),
	}
	if item.Name != nil {
		out.Name = *item.Name
	}
	if item.Type != nil {
		out.Type = *item.Type
	}
	if item.AgentID != uuid.Nil {
		out.AgentId = item.AgentID.String()
	}
	if item.Health != nil {
		out.Health = string(*item.Health)
	}
	if item.Status != nil {
		out.Status = *item.Status
	}
	if item.Tags != nil {
		out.Tags = map[string]string(item.Tags)
	}
	if item.Labels != nil {
		out.Labels = map[string]string(*item.Labels)
	}
	if item.Properties != nil {
		props := map[string]any{}
		for _, p := range *item.Properties {
			props[p.Name] = p.Text
		}
		s, err := structpb.NewStruct(props)
		if err == nil {
			out.Properties = s
		}
	}
	if item.Config != nil && *item.Config != "" {
		var cfg map[string]any
		if err := json.Unmarshal([]byte(*item.Config), &cfg); err == nil {
			s, _ := structpb.NewStruct(cfg)
			out.Config = s
		}
	}
	return out, nil
}
