package views

import (
	"errors"
	"strings"

	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"gorm.io/gorm"
)

const (
	dashboardPropertyKey    = "dashboard.default.view"
	defaultDashboardViewRef = "mission-control-dashboard"
)

// resolveDashboardView reads dashboard.default.view and resolves it to a view model.
// The value can be in "namespace/name" or "name" format.
func resolveDashboardView(ctx context.Context) (*models.View, error) {
	viewRef := ctx.Properties().String(dashboardPropertyKey, defaultDashboardViewRef)
	parts := strings.SplitN(viewRef, "/", 2)

	var view models.View
	query := ctx.DB().Where("deleted_at IS NULL")
	if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
		query = query.Where("namespace = ? AND name = ?", parts[0], parts[1])
	} else {
		query = query.Where("name = ?", viewRef)
	}

	if err := query.First(&view).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, dutyAPI.Errorf(dutyAPI.ENOTFOUND, "dashboard view %s not found", viewRef)
		}
		return nil, ctx.Oops().Wrap(err)
	}

	return &view, nil
}
