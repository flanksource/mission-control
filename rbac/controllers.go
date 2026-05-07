package rbac

import (
	"net/http"

	"github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/incident-commander/db"
	"github.com/labstack/echo/v4"
)

func RegisterRoutes(e *echo.Echo) {
	e.POST("/rbac/:id/update_role", UpdateRoleForUser, Authorization(policy.ObjectRBAC, policy.ActionUpdate))
	e.GET("/rbac/dump", Dump, Authorization(policy.ObjectRBAC, policy.ActionRead))
	e.GET("/rbac/:id/roles", GetRolesForUser, Authorization(policy.ObjectRBAC, policy.ActionRead))
	e.GET("/rbac/token/:id/permissions", GetPermissionsForToken, Authorization(policy.ObjectRBAC, policy.ActionRead))
	e.POST("/rbac/subject-access-reviews", SubjectAccessReviews, Authorization(policy.ObjectRBAC, policy.ActionRead))
	e.POST("/rbac/subject-access-search", SubjectAccessSearch, Authorization(policy.ObjectRBAC, policy.ActionRead))
}

func UpdateRoleForUser(c echo.Context) error {
	userID := c.Param("id")
	reqData := struct {
		Roles []string `json:"roles"`
	}{}
	if err := c.Bind(&reqData); err != nil {
		return c.JSON(http.StatusBadRequest, api.HTTPError{
			Err:     err.Error(),
			Message: "Invalid request body",
		})
	}

	if err := rbac.AddRoleForUser(userID, reqData.Roles...); err != nil {
		return c.JSON(http.StatusInternalServerError, api.HTTPError{
			Err:     err.Error(),
			Message: "Error updating roles",
		})
	}

	return c.JSON(http.StatusOK, api.HTTPSuccess{
		Message: "Role updated successfully",
	})
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

	return c.JSON(http.StatusOK, response)
}

func Dump(c echo.Context) error {
	perms, err := rbac.Enforcer().GetPolicy()
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, policy.NewPermissions(perms))
}
