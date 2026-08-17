package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/flanksource/incident-commander/clientapi"
	clicky "github.com/flanksource/incident-commander/clientcli"
	"github.com/flanksource/incident-commander/clientcli/api"
	"github.com/google/uuid"
)

type catalogItem clientapi.ConfigItem

func (c catalogItem) GetID() string {
	return c.ID.String()
}

func (c catalogItem) GetName() string {
	return stringValue(c.Name, "")
}

// catalogItemDetail keeps the ConfigItem wire shape while expanding its human-readable view.
type catalogItemDetail struct {
	clientapi.ConfigItem `yaml:",inline"`
	Summary              *clientapi.ConfigItemSummary `json:"-" yaml:"-"`
}

func (r catalogItemDetail) MarshalYAML() (any, error) {
	data, err := json.Marshal(r.ConfigItem)
	if err != nil {
		return nil, err
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	return value, nil
}

func (r catalogItemDetail) Pretty() api.Text {
	t := catalogItemSummary(r.ConfigItem).NewLine().Append(catalogItemDetails(r.ConfigItem, r.Summary))

	if r.Properties != nil && len(*r.Properties) > 0 {
		t = t.NewLine().AddText("Properties", "font-bold").NewLine().Append(catalogItemProperties(*r.Properties))
	}

	if r.Config != nil && *r.Config != "" {
		t = t.NewLine().Append(clicky.Collapsed("Config", catalogConfigCodeBlock(*r.Config)))
	}

	return t
}

func (c catalogItem) Pretty() api.Text {
	return catalogItemSummary(clientapi.ConfigItem(c))
}

func (c catalogItem) PrettyRow(_ any) map[string]api.Text {
	item := clientapi.ConfigItem(c)
	row := map[string]api.Text{
		"name":   clicky.Text(stringValue(item.Name, "<unnamed>"), "font-bold"),
		"type":   clicky.Text(stringValue(item.Type, "-"), "text-gray-600"),
		"class":  clicky.Text(item.ConfigClass, "text-blue-600"),
		"health": clicky.Text("", "text-gray-400"),
	}
	if item.Health != nil {
		row["health"] = catalogHealth(*item.Health)
	}
	if item.Status != nil {
		row["status"] = clicky.Text(*item.Status, "text-gray-700")
	}
	if item.CostTotal30d > 0 {
		row["cost"] = clicky.Text(fmt.Sprintf("$%.2f", item.CostTotal30d), "text-green-700")
	}
	if !item.CreatedAt.IsZero() {
		row["age"] = api.Human(time.Since(item.CreatedAt), "text-gray-600")
	}
	return row
}

func catalogItemSummary(c clientapi.ConfigItem) api.Text {
	t := clicky.Text("")
	if c.Health != nil {
		t = t.Add(catalogHealth(*c.Health)).AddText(" ")
	}
	t = t.AddText(stringValue(c.Name, "<unnamed>"), "font-bold")
	if c.Type != nil {
		t = t.AddText(" ").Add(clicky.Text(*c.Type, "text-xs text-gray-600 bg-gray-100").Wrap("(", ")"))
	}
	if c.ConfigClass != "" {
		t = t.AddText(" ").Add(clicky.Text(c.ConfigClass, "text-xs text-blue-600 bg-blue-50"))
	}
	if len(c.Tags) > 0 {
		t = t.NewLine().AddText("  Tags: ", "text-sm text-gray-500")
		for key, value := range c.Tags {
			t = t.Add(clicky.Text(fmt.Sprintf("%s=%s", key, value), "text-xs bg-gray-100 text-gray-700").Wrap("[", "]")).AddText(" ")
		}
	}
	return t
}

func catalogHealth(health string) api.Text {
	switch health {
	case "healthy":
		return clicky.Text("✓ ", "text-green-600").Append(health, "capitalize text-green-600")
	case "unhealthy":
		return clicky.Text("✗ ", "text-red-600").Append(health, "capitalize text-red-600")
	case "warning":
		return clicky.Text("! ", "text-yellow-600").Append(health, "capitalize text-yellow-600")
	case "unknown":
		return clicky.Text("? ", "text-gray-500").Append(health, "capitalize text-gray-500")
	default:
		return clicky.Text(health, "text-gray-500")
	}
}

func catalogItemDetails(c clientapi.ConfigItem, summary *clientapi.ConfigItemSummary) api.DescriptionList {
	items := []api.KeyValuePair{
		{Key: "ID", Value: c.ID.String()},
		{Key: "Type", Value: stringValue(c.Type, "-")},
		{Key: "Class", Value: c.ConfigClass},
		{Key: "Ready", Value: strconv.FormatBool(c.Ready)},
	}

	if c.Health != nil {
		items = append(items, api.KeyValuePair{Key: "Health", Value: catalogHealth(*c.Health)})
	}
	if c.Status != nil {
		items = append(items, api.KeyValuePair{Key: "Status", Value: *c.Status})
	}
	if c.Description != nil && *c.Description != "" {
		items = append(items, api.KeyValuePair{Key: "Description", Value: *c.Description})
	}
	if c.Source != nil && *c.Source != "" {
		items = append(items, api.KeyValuePair{Key: "Source", Value: *c.Source})
	}
	if c.ScraperID != nil && *c.ScraperID != "" {
		items = append(items, api.KeyValuePair{Key: "Scraper", Value: *c.ScraperID})
	}
	if c.AgentID != uuid.Nil {
		items = append(items, api.KeyValuePair{Key: "Agent", Value: c.AgentID.String()})
	}
	if c.Path != "" {
		items = append(items, api.KeyValuePair{Key: "Path", Value: c.Path})
	}
	if c.ParentID != nil {
		items = append(items, api.KeyValuePair{Key: "Parent", Value: c.ParentID.String()})
	}
	if len(c.ExternalID) > 0 {
		items = append(items, api.KeyValuePair{Key: "External ID", Value: strings.Join(c.ExternalID, ", ")})
	}
	if summary != nil {
		if summary.CostPerMinute != nil && *summary.CostPerMinute != 0 {
			items = append(items, api.KeyValuePair{Key: "Cost per Minute", Value: fmt.Sprintf("$%.6f", *summary.CostPerMinute)})
		}
		if summary.CostTotal1h != nil && *summary.CostTotal1h != 0 {
			items = append(items, api.KeyValuePair{Key: "Cost (1h)", Value: fmt.Sprintf("$%.2f", *summary.CostTotal1h)})
		}
		if summary.CostTotal1d != nil && *summary.CostTotal1d != 0 {
			items = append(items, api.KeyValuePair{Key: "Cost (1d)", Value: fmt.Sprintf("$%.2f", *summary.CostTotal1d)})
		}
		if summary.CostTotal30d != nil && *summary.CostTotal30d != 0 {
			items = append(items, api.KeyValuePair{Key: "Cost (30d)", Value: fmt.Sprintf("$%.2f", *summary.CostTotal30d)})
		}
	}
	if !c.CreatedAt.IsZero() {
		items = append(items, api.KeyValuePair{Key: "Created", Value: c.CreatedAt.Format(time.RFC3339)})
	}
	if !c.InsertedAt.IsZero() {
		items = append(items, api.KeyValuePair{Key: "Inserted", Value: c.InsertedAt.Format(time.RFC3339)})
	}
	if c.UpdatedAt != nil {
		items = append(items, api.KeyValuePair{Key: "Updated", Value: c.UpdatedAt.Format(time.RFC3339)})
	}
	if c.DeletedAt != nil {
		items = append(items, api.KeyValuePair{Key: "Deleted", Value: c.DeletedAt.Format(time.RFC3339)})
	}
	if c.DeleteReason != "" {
		items = append(items, api.KeyValuePair{Key: "Delete Reason", Value: c.DeleteReason})
	}
	if c.Labels != nil && len(*c.Labels) > 0 {
		items = append(items, api.KeyValuePair{Key: "Labels", Value: clicky.Map(*c.Labels, "text-xs")})
	}
	if len(c.Tags) > 0 {
		items = append(items, api.KeyValuePair{Key: "Tags", Value: clicky.Map(c.Tags, "text-xs")})
	}

	return api.DescriptionList{Items: items}
}

func catalogItemProperties(properties clientapi.CatalogProperties) api.DescriptionList {
	items := make([]api.KeyValuePair, 0, len(properties))
	for i, property := range properties {
		label := fmt.Sprintf("Property %d", i+1)
		if property != nil {
			if property.Label != "" {
				label = property.Label
			} else if property.Name != "" {
				label = property.Name
			}
		}
		items = append(items, api.KeyValuePair{Key: label, Value: catalogPropertyValue(property)})
	}
	return api.DescriptionList{Items: items}
}

func catalogPropertyValue(property *clientapi.CatalogProperty) string {
	if property == nil {
		return "-"
	}

	value := property.Text
	if value == "" && property.Value != nil {
		value = strconv.FormatInt(*property.Value, 10)
		if property.Max != nil {
			value += "/" + strconv.FormatInt(*property.Max, 10)
		}
	}
	if value == "" && len(property.Links) > 0 {
		value = property.Links[0].URL
	}
	if property.Unit != "" && value != "" {
		value += " " + property.Unit
	}
	if property.Status != "" {
		if value == "" {
			value = property.Status
		} else {
			value += " (" + property.Status + ")"
		}
	}
	if value == "" {
		return "-"
	}
	return value
}

func catalogConfigCodeBlock(configJSON string) api.Code {
	var parsed any
	if err := json.Unmarshal([]byte(configJSON), &parsed); err == nil {
		if pretty, err := json.MarshalIndent(parsed, "", "  "); err == nil {
			configJSON = string(pretty)
		}
	}
	return api.CodeBlock("json", configJSON)
}

func stringValue(value *string, fallback string) string {
	if value == nil {
		return fallback
	}
	return *value
}
