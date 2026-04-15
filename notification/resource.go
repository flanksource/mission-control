package notification

import (
	"errors"
	"strings"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
)

const ResourceNoLongerExistsReason = "resource no longer exists"

var ErrStaleResourceEvent = errors.New(ResourceNoLongerExistsReason)

func IsStaleResourceEventError(err error) bool {
	return errors.Is(err, ErrStaleResourceEvent)
}

// ResourceExists reports whether the resource referenced by a notification
// still exists in the database.
func ResourceExists(ctx context.Context, sourceEvent string, resourceID uuid.UUID) (bool, error) {
	if resourceID == uuid.Nil {
		return true, nil
	}

	var table string
	switch {
	case strings.HasPrefix(sourceEvent, "config."):
		table = (&models.ConfigItem{}).TableName()
	case strings.HasPrefix(sourceEvent, "component."):
		table = (&models.Component{}).TableName()
	case strings.HasPrefix(sourceEvent, "check."):
		table = (&models.Check{}).TableName()
	case strings.HasPrefix(sourceEvent, "canary."):
		table = (&models.Canary{}).TableName()
	default:
		return true, nil
	}

	var count int64
	if err := ctx.DB().Table(table).
		Where("id = ?", resourceID).
		Where("deleted_at IS NULL").
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}
