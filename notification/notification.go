package notification

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/flanksource/commons/text"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/utils"
)

var (
	notificationByIDCache = utils.NewGenCache(time.Hour*12, time.Hour*1)

	// a separate cache because we purge the caches in two different ways.
	notificationByEventCache = utils.NewGenCache(time.Hour*12, time.Hour*1)
)

func PurgeCache(notificationID string) {
	notificationByEventCache.Flush()
	notificationByIDCache.Delete(notificationID)
}

// GetNotificationIDsForEvent returns ids of all the notifications
// that are watching the given event.
func GetNotificationIDsForEvent(ctx context.Context, eventName string) ([]string, error) {
	return utils.GetOrLoad(notificationByEventCache, eventName, func() ([]string, error) {
		var ids []string
		if err := ctx.DB().Model(&models.Notification{}).Where("deleted_at IS NULL").Where("? = ANY(events)", eventName).Pluck("id", &ids).Error; err != nil {
			return nil, err
		}
		return ids, nil
	})
}

// A wrapper around notification that also contains the custom notifications.
type NotificationWithSpec struct {
	models.Notification

	RepeatInterval             *time.Duration
	CustomNotifications        []api.NotificationConfig
	FallbackCustomNotification *api.NotificationConfig
	Inhibitions                []v1.NotificationInihibition
}

func GetNotification(ctx context.Context, id string) (*NotificationWithSpec, error) {
	return utils.GetOrLoad(notificationByIDCache, id, func() (*NotificationWithSpec, error) {
		var n models.Notification
		if err := ctx.DB().Where("id = ?", id).Find(&n).Error; err != nil {
			return nil, err
		}

		b, err := json.Marshal(n.CustomServices)
		if err != nil {
			return nil, err
		}

		var customNotifications []api.NotificationConfig
		if err := json.Unmarshal(b, &customNotifications); err != nil {
			return nil, err
		}

		data := NotificationWithSpec{
			Notification:        n,
			CustomNotifications: customNotifications,
		}

		if len(n.FallbackCustomServices) > 0 {
			b, err := json.Marshal(n.FallbackCustomServices)
			if err != nil {
				return nil, err
			}

			var customNotifications []api.NotificationConfig
			if err := json.Unmarshal(b, &customNotifications); err != nil {
				return nil, err
			}

			if len(customNotifications) > 0 {
				data.FallbackCustomNotification = &customNotifications[0]
			}
		}

		if n.RepeatInterval != "" {
			interval, err := text.ParseDuration(n.RepeatInterval)
			if err != nil {
				return nil, fmt.Errorf("error parsing repeat interval[%s] to time.Duration: %w", n.RepeatInterval, err)
			}
			data.RepeatInterval = interval
		}

		if len(n.Inhibitions) > 0 {
			if err := json.Unmarshal(n.Inhibitions, &data.Inhibitions); err != nil {
				return nil, fmt.Errorf("error parsing inhibitions[%s] to NotificationInihibition: %w", n.Inhibitions, err)
			}
		}

		return &data, nil
	})
}
