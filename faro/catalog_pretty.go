package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
)

// catalogItemDetail keeps the ConfigItem wire shape while expanding its human-readable view.
type catalogItemDetail struct {
	models.ConfigItem `yaml:",inline"`
}

func (r catalogItemDetail) Pretty() api.Text {
	t := r.ConfigItem.Pretty().NewLine().Append(catalogItemDetails(r.ConfigItem))

	if r.Properties != nil && len(*r.Properties) > 0 {
		t = t.NewLine().AddText("Properties", "font-bold").NewLine().Append(catalogItemProperties(*r.Properties))
	}

	if r.Config != nil && *r.Config != "" {
		t = t.NewLine().Append(clicky.Collapsed("Config", catalogConfigCodeBlock(*r.Config)))
	}

	return t
}

func catalogItemDetails(c models.ConfigItem) api.DescriptionList {
	items := []api.KeyValuePair{
		{Key: "ID", Value: c.ID.String()},
		{Key: "Type", Value: stringValue(c.Type, "-")},
		{Key: "Class", Value: c.ConfigClass},
		{Key: "Ready", Value: strconv.FormatBool(c.Ready)},
	}

	if c.Health != nil {
		items = append(items, api.KeyValuePair{Key: "Health", Value: c.Health.Pretty()})
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
	if c.CostPerMinute != 0 {
		items = append(items, api.KeyValuePair{Key: "Cost per Minute", Value: fmt.Sprintf("$%.6f", c.CostPerMinute)})
	}
	if c.CostTotal1d != 0 {
		items = append(items, api.KeyValuePair{Key: "Cost (1d)", Value: fmt.Sprintf("$%.2f", c.CostTotal1d)})
	}
	if c.CostTotal7d != 0 {
		items = append(items, api.KeyValuePair{Key: "Cost (7d)", Value: fmt.Sprintf("$%.2f", c.CostTotal7d)})
	}
	if c.CostTotal30d != 0 {
		items = append(items, api.KeyValuePair{Key: "Cost (30d)", Value: fmt.Sprintf("$%.2f", c.CostTotal30d)})
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

func catalogItemProperties(properties types.Properties) api.DescriptionList {
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

func catalogPropertyValue(property *types.Property) string {
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
