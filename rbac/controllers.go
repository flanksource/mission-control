package rbac

import (
	"net/http"
	"strings"

	"github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/rbac/adapter"
	"github.com/labstack/echo/v4"
	"github.com/samber/lo"
)

func RegisterRoutes(e *echo.Echo) {
	e.GET("/rbac/dump", Dump, Authorization(policy.ObjectRBAC, policy.ActionRead))
	e.GET("/rbac/:id/roles", GetRolesForUser, Authorization(policy.ObjectRBAC, policy.ActionRead))
	e.GET("/rbac/token/:id/permissions", GetPermissionsForToken, Authorization(policy.ObjectRBAC, policy.ActionRead))
	e.POST("/rbac/subject-access-reviews", SubjectAccessReviews, Authorization(policy.ObjectRBAC, adapter.ActionAccessReview))
	e.POST("/rbac/subject-access-search", SubjectAccessSearch, Authorization(policy.ObjectRBAC, adapter.ActionAccessReview))
	registerManagedKinds(e)
}

func GetRolesForUser(c echo.Context) error {
	userID := c.Param("id")
	roles, err := rbac.RolesForUser(userID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, api.HTTPError{
			Err:     err.Error(),
			Message: "Error getting roles",
		})
	}
	return c.JSON(http.StatusOK, api.HTTPSuccess{
		Payload: roles,
	})
}

func GetPermissionsForToken(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)
	tokenID := c.Param("id")
	token, err := db.GetAccessToken(ctx, tokenID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, api.HTTPError{
			Err:     err.Error(),
			Message: "Error getting token",
		})
	}

	perms, err := rbac.PermsForUser(token.PersonID.String())
	if err != nil {
		return c.JSON(http.StatusInternalServerError, api.HTTPError{
			Err:     err.Error(),
			Message: "Error getting permissions",
		})
	}
	return c.JSON(http.StatusOK, api.HTTPSuccess{
		Payload: perms,
	})
}

type SubjectAccessReviewResponse struct {
	Resource SubjectAccessReviewResource `json:"resource"`
	Action   string                      `json:"action"`
	Results  []SubjectAccessReviewResult `json:"results"`
}

func SubjectAccessReviews(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	var req SubjectAccessReviewRequest
	if err := c.Bind(&req); err != nil {
		return api.WriteError(c, api.Errorf(api.EINVALID, "invalid request body: %v", err))
	}

	if err := req.Validate(ctx); err != nil {
		return api.WriteError(c, err)
	}

	// A Role's limit principal isn't a subject. Checking it would join it to everyone, widening every token it limits.
	for _, subject := range req.Subjects {
		if strings.HasPrefix(subject, adapter.RoleLimitPrincipalPrefix) {
			return api.WriteError(c, api.Errorf(api.EINVALID, "%q isn't a subject", subject))
		}
	}

	results, err := rbac.RunSubjectAccessReview(ctx, req)
	if err != nil {
		return api.WriteError(c, err)
	}

	return c.JSON(http.StatusOK, SubjectAccessReviewResponse{
		Resource: req.Resource,
		Action:   req.Action,
		Results:  results,
	})
}

func SubjectAccessSearch(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	var req SubjectAccessSearchRequest
	if err := c.Bind(&req); err != nil {
		return api.WriteError(c, api.Errorf(api.EINVALID, "invalid request body: %v", err))
	}

	response, err := rbac.RunSubjectAccessSearch(ctx, req)
	if err != nil {
		return api.WriteError(c, err)
	}

	response.Results, err = readableSearchResults(ctx, response.Results)
	if err != nil {
		return api.WriteError(c, err)
	}
	response.Total = len(response.Results)

	return c.JSON(http.StatusOK, response)
}

// readableSearchResults keeps the results of a subject access search that the reviewer may read themselves,
// so a review never names resources the reviewer can't see.
func readableSearchResults(ctx context.Context, results []rbac.SubjectAccessSearchResult) ([]rbac.SubjectAccessSearchResult, error) {
	ids := map[string][]string{}
	for _, r := range results {
		ids[r.ResourceType] = append(ids[r.ResourceType], r.ID)
	}

	readable := map[string]bool{}
	if len(ids["playbook"]) > 0 {
		var playbooks []models.Playbook
		if err := ctx.DB().Where("id IN ?", ids["playbook"]).Find(&playbooks).Error; err != nil {
			return nil, ctx.Oops().Wrap(err)
		}
		for _, playbook := range playbooks {
			if HasPermission(ctx, ctx.Subject(), &models.ABACAttribute{Playbook: playbook}, policy.ActionRead) {
				readable[playbook.ID.String()] = true
			}
		}
	}

	if len(ids["connection"]) > 0 {
		var connections []models.Connection
		if err := ctx.DB().Where("id IN ?", ids["connection"]).Find(&connections).Error; err != nil {
			return nil, ctx.Oops().Wrap(err)
		}
		for _, connection := range connections {
			if AuthorizeConnection(ctx, connection, policy.ActionRead) == nil {
				readable[connection.ID.String()] = true
			}
		}
	}

	// No Role grants reading views yet, so no view is readable
	return lo.Filter(results, func(r rbac.SubjectAccessSearchResult, _ int) bool { return readable[r.ID] }), nil
}

func Dump(c echo.Context) error {
	perms, err := rbac.Enforcer().GetPolicy()
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, policy.NewPermissions(perms))
}
