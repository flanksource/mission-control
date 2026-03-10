package views

import (
	"net/http"
	"strings"

	"github.com/flanksource/commons/logger"
	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"golang.org/x/sync/errgroup"

	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	echoSrv "github.com/flanksource/incident-commander/echo"
	"github.com/flanksource/incident-commander/rbac"
)

const (
	dashboardPropertyKey     = "dashboard.default.view"
	defaultDashboardViewName = "mission-control-dashboard"
)

func init() {
	echoSrv.RegisterRoutes(registerDashboardRoutes)
}

func registerDashboardRoutes(e *echo.Echo) {
	logger.Infof("Registering /dashboard route")
	e.GET("/dashboard", HandleGetDashboard, rbac.Authorization(policy.ObjectViews, policy.ActionRead))
}

type DashboardResponse struct {
	View     *DashboardViewInfo         `json:"view"`
	Sections map[string]*api.ViewResult `json:"sections"`
}

type DashboardViewInfo struct {
	ID        string      `json:"id"`
	Name      string      `json:"name"`
	Namespace string      `json:"namespace"`
	Title     string      `json:"title,omitempty"`
	Icon      string      `json:"icon,omitempty"`
	Spec      v1.ViewSpec `json:"spec"`
}

func HandleGetDashboard(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	viewModel, err := resolveDashboardView(ctx)
	if err != nil {
		return dutyAPI.WriteError(c, err)
	}

	attr := &models.ABACAttribute{View: *viewModel}
	if !dutyRBAC.HasPermission(ctx, ctx.Subject(), attr, policy.ActionRead) {
		return dutyAPI.WriteError(c, ctx.Oops().Code(dutyAPI.EFORBIDDEN).Errorf("access denied to dashboard view %s/%s", viewModel.Namespace, viewModel.Name))
	}

	dashboardResult, err := ReadOrPopulateViewTable(ctx, viewModel.Namespace, viewModel.Name)
	if err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrap(err))
	}

	view, err := db.GetView(ctx, viewModel.Namespace, viewModel.Name)
	if err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrap(err))
	}

	response := DashboardResponse{
		View: &DashboardViewInfo{
			ID:        viewModel.ID.String(),
			Name:      viewModel.Name,
			Namespace: viewModel.Namespace,
			Title:     dashboardResult.Title,
			Icon:      dashboardResult.Icon,
			Spec:      view.Spec,
		},
		Sections: make(map[string]*api.ViewResult),
	}

	type sectionFetchResult struct {
		name   string
		result *api.ViewResult
	}

	eg := errgroup.Group{}
	eg.SetLimit(10)
	results := make(chan sectionFetchResult, len(view.Spec.Sections))

	for _, section := range view.Spec.Sections {
		if section.ViewRef == nil {
			continue
		}

		eg.Go(func() error {
			result, err := fetchSection(ctx, section.ViewRef.Namespace, section.ViewRef.Name)
			if err != nil {
				ctx.Logger.Warnf("failed to fetch section %s/%s: %v", section.ViewRef.Namespace, section.ViewRef.Name, err)
				return nil
			}
			if result != nil {
				results <- sectionFetchResult{name: section.ViewRef.Name, result: result}
			}
			return nil
		})
	}

	go func() {
		_ = eg.Wait()
		close(results)
	}()

	for sr := range results {
		response.Sections[sr.name] = sr.result
	}

	return c.JSON(http.StatusOK, response)
}

// resolveDashboardView reads the dashboard.default.view property and resolves
// it to a view model. The property value can be a UUID, a "namespace/name"
// string, or a bare name. Falls back to "mission-control-dashboard" when unset.
func resolveDashboardView(ctx context.Context) (*models.View, error) {
	propValue := ctx.Properties().String(dashboardPropertyKey, "")
	if propValue == "" {
		propValue = defaultDashboardViewName
	}

	var view models.View

	// Try UUID
	if uid, parseErr := uuid.Parse(propValue); parseErr == nil {
		if err := ctx.DB().Where("id = ? AND deleted_at IS NULL", uid).Find(&view).Error; err != nil {
			return nil, ctx.Oops().Wrap(err)
		} else if view.ID == uuid.Nil {
			return nil, dutyAPI.Errorf(dutyAPI.ENOTFOUND, "dashboard view (id=%s) not found", propValue)
		}
		return &view, nil
	}

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
	if err := ctx.DB().Where("name = ? AND deleted_at IS NULL", propValue).First(&view).Error; err != nil {
		return nil, ctx.Oops().Wrap(err)
	}
	return &view, nil
}

// fetchSection fetches a section's view definition (same as POST /view/{namespace}/{name}).
func fetchSection(ctx context.Context, namespace, name string) (*api.ViewResult, error) {
	var viewModel models.View
	if err := ctx.DB().Select("id, namespace, name").
		Where("name = ? AND namespace = ? AND deleted_at IS NULL", name, namespace).
		Find(&viewModel).Error; err != nil {
		return nil, ctx.Oops().Wrap(err)
	} else if viewModel.ID == uuid.Nil {
		return nil, dutyAPI.Errorf(dutyAPI.ENOTFOUND, "section view %s/%s not found", namespace, name)
	}

	attr := &models.ABACAttribute{View: viewModel}
	if !dutyRBAC.HasPermission(ctx, ctx.Subject(), attr, policy.ActionRead) {
		return nil, nil
	}

	return ReadOrPopulateViewTable(ctx, namespace, name)
}
