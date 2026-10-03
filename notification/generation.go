package notification

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/flanksource/duty/models"
)

const staleGenerationReason = "resource generation changed before waitFor elapsed"

func getConfigGeneration(config *models.ConfigItem) string {
	if config == nil || config.Config == nil || *config.Config == "" {
		return ""
	}

	var obj map[string]any
	if err := json.Unmarshal([]byte(*config.Config), &obj); err != nil {
		return ""
	}

	metadata, ok := obj["metadata"].(map[string]any)
	if !ok {
		return ""
	}

	return generationToString(metadata["generation"])
}

func generationToString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		return strconv.FormatInt(int64(t), 10)
	case float32:
		return strconv.FormatInt(int64(t), 10)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case int32:
		return strconv.FormatInt(int64(t), 10)
	case json.Number:
		return t.String()
	default:
		return fmt.Sprint(t)
	}
}

func shouldSkipDueToGeneration(payload NotificationEventPayload, celEnv *celVariables) bool {
	if payload.ResourceGeneration == "" || celEnv == nil || celEnv.ConfigItem == nil {
		return false
	}

	currentGeneration := getConfigGeneration(celEnv.ConfigItem)
	return currentGeneration != "" && currentGeneration != payload.ResourceGeneration
}

func generationChangedMessage(payload NotificationEventPayload, celEnv *celVariables) string {
	return fmt.Sprintf("%s: previous=%s current=%s", staleGenerationReason, payload.ResourceGeneration, getConfigGeneration(celEnv.ConfigItem))
}
