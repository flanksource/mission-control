package v1

import (
	"encoding/json"
	"fmt"

	"github.com/flanksource/incident-commander/config/schemas"
)

// ParseAndValidatePlaybookSpec applies the generated schema and semantic validation to a raw playbook spec.
func ParseAndValidatePlaybookSpec(data []byte) (*PlaybookSpec, error) {
	if validationErr, err := schemas.ValidatePlaybookSpec(data); err != nil {
		return nil, err
	} else if validationErr != nil {
		return nil, validationErr
	}

	var spec PlaybookSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("invalid playbook spec: %w", err)
	}
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	return &spec, nil
}
