package rbac

import (
	"net/http"

	"github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/rbac/adapter"
	"github.com/labstack/echo/v4"
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

	// The token's limit: the policies of the RoleBindings that select it. What it may do is also capped by its owner's.
	perms, err := rbac.PermsForUser(models.AccessTokenPrincipal(token.ID))
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

func SubjectAccessReviews(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	var req SubjectAccessReviewRequest
	if err := c.Bind(&req); err != nil {
		return api.WriteError(c, api.Errorf(api.EINVALID, "invalid request body: %v", err))
	}

	response, err := RunSubjectAccessReview(ctx, req)
	if err != nil {
		return api.WriteError(c, err)
	}

	return c.JSON(http.StatusOK, response)
}

func SubjectAccessSearch(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	var req SubjectAccessSearchRequest
	if err := c.Bind(&req); err != nil {
		return api.WriteError(c, api.Errorf(api.EINVALID, "invalid request body: %v", err))
	}

	response, err := RunSubjectAccessSearch(ctx, req)
	if err != nil {
		return api.WriteError(c, err)
	}

	return c.JSON(http.StatusOK, response)
}

func Dump(c echo.Context) error {
	perms, err := rbac.Enforcer().GetPolicy()
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, policy.NewPermissions(perms))
}
