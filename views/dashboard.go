package views

import (
	"strings"

	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
)

const (
	dashboardPropertyKey    = "dashboard.default.view"
	defaultDashboardViewRef = "mc/mission-control-dashboard"
)

// resolveDashboardView reads dashboard.default.view and resolves it to a view model.
// The value must be in "namespace/name" format.
func resolveDashboardView(ctx context.Context) (*models.View, error) {
	viewRef := ctx.Properties().String(dashboardPropertyKey, defaultDashboardViewRef)
	parts := strings.SplitN(viewRef, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, dutyAPI.Errorf(dutyAPI.EINVALID, "%s must be in namespace/name format", dashboardPropertyKey)
	}

	var view models.View
	if err := ctx.DB().Where("namespace = ? AND name = ? AND deleted_at IS NULL", parts[0], parts[1]).Find(&view).Error; err != nil {
		return nil, ctx.Oops().Wrap(err)
	} else if view.ID == uuid.Nil {
		return nil, dutyAPI.Errorf(dutyAPI.ENOTFOUND, "dashboard view %s/%s not found", parts[0], parts[1])
	}

	return &view, nil
}
