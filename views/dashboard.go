package views

import (
	"strings"

	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
)

const (
	dashboardPropertyKey     = "dashboard.default.view"
	defaultDashboardViewName = "mission-control-dashboard"
)

// resolveDashboardView reads the dashboard.default.view property and resolves
// it to a view model. The property value can be a UUID, a "namespace/name"
// string, or a bare name. Falls back to "mission-control-dashboard" when unset.
func resolveDashboardView(ctx context.Context) (*models.View, error) {
	propValue := ctx.Properties().String(dashboardPropertyKey, "")
	if propValue == "" {
		propValue = defaultDashboardViewName
	}

	var view models.View

	// Try namespace/name
	if parts := strings.SplitN(propValue, "/", 2); len(parts) == 2 {
		if err := ctx.DB().Where("namespace = ? AND name = ? AND deleted_at IS NULL", parts[0], parts[1]).Find(&view).Error; err != nil {
			return nil, ctx.Oops().Wrap(err)
		} else if view.ID == uuid.Nil {
			return nil, dutyAPI.Errorf(dutyAPI.ENOTFOUND, "dashboard view %s/%s not found", parts[0], parts[1])
		}
		return &view, nil
	}

	// Bare name
	if err := ctx.DB().Where("name = ? AND deleted_at IS NULL", propValue).Find(&view).Error; err != nil {
		return nil, ctx.Oops().Wrap(err)
	}

	return &view, nil
}
