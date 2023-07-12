package notification

import (
	"time"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	"github.com/patrickmn/go-cache"
)

var notificationsCache = cache.New(time.Hour*1, time.Hour*1)

func PurgeCache(notificationID string) {
	notificationsCache.Flush()
}

func GetNotifications(ctx *api.Context, eventName string) ([]models.Notification, error) {
	if val, found := notificationsCache.Get(eventName); found {
		return val.([]models.Notification), nil
	}

	var notifications []models.Notification
	if err := ctx.DB().Where("deleted_at IS NULL").Where("? = ANY(events)", eventName).Find(&notifications).Error; err != nil {
		return nil, err
	}
	notificationsCache.Set(eventName, notifications, cache.DefaultExpiration)

	return notifications, nil
}
