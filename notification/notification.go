package notification

import (
	"encoding/json"
	"time"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	"github.com/patrickmn/go-cache"
)

var (
	notificationByIDCache = cache.New(time.Hour*1, time.Hour*1)

	// a separate cache because we purge the caches in two different ways.
	notificationByEventCache = cache.New(time.Hour*1, time.Hour*1)
)

func PurgeCache(notificationID string) {
	notificationByEventCache.Flush()
	notificationByIDCache.Delete(notificationID)
}

func GetNotifications(ctx *api.Context, eventName string) ([]models.Notification, error) {
	if val, found := notificationByEventCache.Get(eventName); found {
		return val.([]models.Notification), nil
	}

	var notifications []models.Notification
	if err := ctx.DB().Where("deleted_at IS NULL").Where("? = ANY(events)", eventName).Find(&notifications).Error; err != nil {
		return nil, err
	}
	notificationByEventCache.Set(eventName, notifications, cache.DefaultExpiration)

	return notifications, nil
}

// A wrapper around notification that also contains the custom notifications.
type NotificationWithSpec struct {
	models.Notification
	CustomNotifications []api.NotificationConfig
}

func GetNotification(ctx *api.Context, id string) (*NotificationWithSpec, error) {
	if val, found := notificationByIDCache.Get(id); found {
		return val.(*NotificationWithSpec), nil
	}

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

	notificationByIDCache.Set(id, &data, cache.DefaultExpiration)

	return &data, nil
}
