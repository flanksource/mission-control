package logs

import (
	"fmt"
	"slices"

	"github.com/flanksource/commons/utils"
	icUtils "github.com/flanksource/incident-commander/utils"
)

// MapFieldToLogLine maps a given key-value pair to the appropriate field in the LogLine struct
// based on the provided FieldMappingConfig.
func MapFieldToLogLine(key string, value any, line *LogLine, config FieldMappingConfig) error {
	if slices.Contains(config.Ignore, key) {
		return nil
	}

	if slices.Contains(config.ID, key) {
		if id, ok := value.(string); ok {
			line.ID, _ = utils.Stringify(id)
		}

		return nil
	}

	if slices.Contains(config.Message, key) {
		if msg, ok := value.(string); ok {
			line.Message, _ = utils.Stringify(msg)
		}

		return nil
	}

	if slices.Contains(config.Timestamp, key) {
		if t := icUtils.ParseTime(fmt.Sprintf("%v", value)); t != nil {
			line.FirstObserved = *t
		}

		return nil
	}

	if slices.Contains(config.Severity, key) {
		if sev, ok := value.(string); ok {
			line.Severity, _ = utils.Stringify(sev)
		}

		return nil
	}

	if slices.Contains(config.Source, key) {
		if src, ok := value.(string); ok {
			line.Source, _ = utils.Stringify(src)
		}

		return nil
	}

	if line.Labels == nil {
		line.Labels = make(map[string]string)
	}

	mm, err := flatMap(key, value)
	if err != nil {
		return fmt.Errorf("error flattening field %s: %w", key, err)
	}
	for k, v := range mm {
		line.Labels[k] = v
	}

	return nil
}

// flatMap flattens a nested structure (like map[string]any) into a flat map[string]string.
// Nested keys are joined with dots.
func flatMap(prefix string, v any) (map[string]string, error) {
	if v == nil {
		return nil, nil
	}

	m := make(map[string]string)
	switch vv := v.(type) {
	case map[string]any:
		for k, val := range vv {
			subMap, err := flatMap(k, val)
			if err != nil {
				return nil, err
			}

			for subK, subV := range subMap {
				key := fmt.Sprintf("%s.%s", prefix, subK)
				if prefix == "" {
					key = subK
				}
				m[key] = subV
			}
		}

	default:
		// Use Stringify for better JSON-like representation, fallback to Sprintf
		if vvJSON, err := utils.Stringify(vv); err == nil {
			m[prefix] = vvJSON
		} else {
			m[prefix] = fmt.Sprintf("%v", vv)
		}
	}

	return m, nil
}
