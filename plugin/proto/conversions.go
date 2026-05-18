package pluginpb

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	structpb "google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
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

// ResourceSelectorFromDuty converts duty's ResourceSelector into its protobuf representation.
func ResourceSelectorFromDuty(selector types.ResourceSelector) *ResourceSelector {
	return &ResourceSelector{
		Agent:          selector.Agent,
		Scope:          selector.Scope,
		Cache:          selector.Cache,
		Search:         selector.Search,
		Limit:          int32(selector.Limit),
		IncludeDeleted: selector.IncludeDeleted,
		Id:             selector.ID,
		Name:           selector.Name,
		Namespace:      selector.Namespace,
		TagSelector:    selector.TagSelector,
		LabelSelector:  selector.LabelSelector,
		FieldSelector:  selector.FieldSelector,
		Health:         string(selector.Health),
		Types:          []string(selector.Types),
		Statuses:       []string(selector.Statuses),
	}
}

// ConnectionToProto converts a duty connection into its protobuf representation.
func ConnectionToProto(conn *models.Connection) *ResolvedConnection {
	props := map[string]any{
		"type":        conn.Type,
		"name":        conn.Name,
		"namespace":   conn.Namespace,
		"insecureTLS": conn.InsecureTLS,
	}
	for k, v := range conn.Properties {
		props[k] = v
	}
	if conn.URL != "" {
		if _, ok := props["endpoint"]; !ok {
			props["endpoint"] = conn.URL
		}
	}
	if conn.Certificate != "" && connectionTypeMatches("kubernetes", conn.Type) {
		props["kubeconfig"] = conn.Certificate
	}
	pbProps, _ := structpb.NewStruct(props)

	return &ResolvedConnection{
		Type:        conn.Type,
		Url:         conn.URL,
		Username:    conn.Username,
		Password:    conn.Password,
		Certificate: conn.Certificate,
		Token:       connectionToken(conn),
		Properties:  pbProps,
		ExpiresAt:   timestamppb.New(time.Now().Add(5 * time.Minute)),
	}
}

func connectionTypeMatches(requested, actual string) bool {
	requested = strings.ToLower(strings.ReplaceAll(requested, "-", "_"))
	actual = strings.ToLower(strings.ReplaceAll(actual, "-", "_"))
	if requested == actual {
		return true
	}
	if requested == "sql" {
		switch actual {
		case "postgres", "postgresql", "mysql", "mssql", "sql_server", "sqlserver":
			return true
		}
	}
	return false
}

func connectionToken(conn *models.Connection) string {
	for _, key := range []string{"token", "sessionToken", "session_token"} {
		if token := conn.Properties[key]; token != "" {
			return token
		}
	}
	return ""
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
