package views

import (
	"fmt"
	"net/http"

	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"golang.org/x/sync/errgroup"

	"github.com/flanksource/incident-commander/api"
)

// ViewMetadataResponse is the response for the view metadata endpoint.
// It includes the view definition and all resolved section definitions.
type ViewMetadataResponse struct {
	*api.ViewResult `json:",inline"`
	Sections        map[string]*api.ViewResult `json:"sectionResults,omitempty"`
}

func HandleGetViewMetadataByID(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	id := c.Param("id")

	var view models.View
	if err := ctx.DB().Where("id = ? AND deleted_at IS NULL", id).Find(&view).Error; err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrap(err))
	} else if view.ID == uuid.Nil {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.ENOTFOUND, "view(id=%s) not found", id))
	}

	return handleViewMetadata(ctx, c, view)
}

func HandleGetViewMetadataByNamespaceName(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	namespace := c.Param("namespace")
	name := c.Param("name")

	var view models.View
	if err := ctx.DB().Where("namespace = ? AND name = ? AND deleted_at IS NULL", namespace, name).Find(&view).Error; err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrap(err))
	} else if view.ID == uuid.Nil {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.ENOTFOUND, "view(namespace=%s, name=%s) not found", namespace, name))
	}

	return handleViewMetadata(ctx, c, view)
}

func handleViewMetadata(ctx context.Context, c echo.Context, viewModel models.View) error {
	attr := &models.ABACAttribute{View: viewModel}
	if !dutyRBAC.HasPermission(ctx, ctx.Subject(), attr, policy.ActionRead) {
		return dutyAPI.WriteError(c, ctx.Oops().Code(dutyAPI.EFORBIDDEN).Errorf("access denied to view %s/%s", viewModel.Namespace, viewModel.Name))
	}

	response, err := GetViewMetadata(ctx, viewModel.Namespace, viewModel.Name)
	if err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrap(err))
	}

	return c.JSON(http.StatusOK, response)
}

// GetViewMetadata fetches a view definition and resolves all its sections
// in parallel. Each section with a viewRef gets its own ViewResult.
func GetViewMetadata(ctx context.Context, namespace, name string) (*ViewMetadataResponse, error) {
	viewResult, err := ReadOrPopulateViewTable(ctx, namespace, name)
	if err != nil {
		return nil, fmt.Errorf("failed to get view %s/%s: %w", namespace, name, err)
	}

	response := &ViewMetadataResponse{
		ViewResult: viewResult,
	}

	var viewRefs []api.ViewSection
	for _, section := range viewResult.Sections {
		if section.ViewRef != nil {
			viewRefs = append(viewRefs, section)
		}
	}

	if len(viewRefs) == 0 {
		return response, nil
	}

	response.Sections = make(map[string]*api.ViewResult, len(viewRefs))

	type sectionFetchResult struct {
		name   string
		result *api.ViewResult
	}

	eg := errgroup.Group{}
	eg.SetLimit(10)
	results := make(chan sectionFetchResult, len(viewRefs))

	for _, section := range viewRefs {
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

	return response, nil
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
