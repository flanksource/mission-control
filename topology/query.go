package topology

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
)

func QueryRenderComponents(ctx context.Context, systemTemplateID string) ([]api.Renderers, error) {
	rows, err := db.Gorm.WithContext(ctx).Table("templates").Select("spec->'renderers'").Where("id = ?", systemTemplateID).Rows()
	if err != nil {
		return nil, fmt.Errorf("failed to query renderers(); %w", err)
	}
	defer rows.Close()

	var results []api.Renderers
	for rows.Next() {
		var renderer api.Renderers
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, fmt.Errorf("rows.Scan(); %w", err)
		}

		if err := json.Unmarshal([]byte(s), &renderer); err != nil {
			return nil, fmt.Errorf("json.Unmarshal(); %w", err)
		}
		results = append(results, renderer)
	}

	return results, nil
}
