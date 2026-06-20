package tools

import (
	"encoding/json"
	"fmt"
)

const ToolCustomSchema = "extract_structured_output"

func CustomSchema(schemaJSON string) (map[string]any, error) {
	var schema map[string]any
	if err := json.Unmarshal([]byte(schemaJSON), &schema); err != nil {
		return nil, fmt.Errorf("failed to parse custom schema JSON: %w", err)
	}

	return schema, nil
}
