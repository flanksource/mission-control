package views

import (
	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/incident-commander/api"
	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
)

// ViewMetadataResponse is the response for the view metadata endpoint.
// It includes the view definition and all resolved section definitions.
type ViewMetadataResponse struct {
	ID              string `json:"id"`
	*api.ViewResult `json:",inline"`
	Sections        map[string]*api.ViewResult `json:"sectionResults,omitempty"`
}

// getViewMetadata fetches a view definition and resolves all its sections
// in parallel. Each section with a viewRef gets its own ViewResult.
//
// Permission checks for the root view happen here.
func getViewMetadata(ctx context.Context, viewModel models.View) (*ViewMetadataResponse, error) {
	attr := &models.ABACAttribute{View: viewModel}
	if !dutyRBAC.HasPermission(ctx, ctx.Subject(), attr, policy.ActionRead) {
		return nil, ctx.Oops().Code(dutyAPI.EFORBIDDEN).Errorf("access denied to view %s/%s", viewModel.Namespace, viewModel.Name)
	}

	viewResult, err := ReadOrPopulateViewTable(ctx, viewModel.Namespace, viewModel.Name)
	if err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to get view %s/%s", viewModel.Namespace, viewModel.Name)
	}

	response := &ViewMetadataResponse{
		ID:         viewModel.ID.String(),
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
