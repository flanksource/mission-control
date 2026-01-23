package notification

import (
	"encoding/json"

	"github.com/flanksource/duty/types"
)

func storeNotificationPayload(ctx *Context, payload NotificationMessagePayload) {
	b, err := json.Marshal(payload)
	if err != nil {
		ctx.Logger.Warnf("failed to marshal notification payload: %v", err)
		return
	}
	ctx.WithBodyPayload(types.JSON(b))
}
