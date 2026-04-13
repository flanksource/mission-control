package rbac

import (
	"net/http"

	"github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
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

const maxSubjectAccessReviewSubjects = 500

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

	results, err := runSubjectAccessReview(ctx, req)
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

	if err := req.Validate(); err != nil {
		return api.WriteError(c, err)
	}

	results := make([]SubjectAccessSearchResult, 0)

	for _, resourceType := range req.ResourceTypes {
		switch resourceType {
		case "playbook":
			var playbooks []models.Playbook
			query := ctx.DB().Select("id").Model(&models.Playbook{}).Where("deleted_at IS NULL")
			if err := query.Order("COALESCE(title, name) ASC").Find(&playbooks).Error; err != nil {
				return api.WriteError(c, ctx.Oops().Wrapf(err, "failed to list playbooks for subject access search"))
			}

			for _, playbook := range playbooks {
				r, err := runSubjectAccessReview(ctx, SubjectAccessReviewRequest{
					Subjects: []string{req.Subject},
					Action:   req.Action,
					Resource: SubjectAccessReviewResource{
						Playbook: playbook.ID.String(),
					},
				})
				if err != nil {
					return api.WriteError(c, err)
				}

				for _, x := range r {
					if x.Allowed {
						results = append(results, SubjectAccessSearchResult{
							ResourceType: "playbook",
							ID:           playbook.ID.String(),
						})
					}
				}
			}

		case "view":
			var views []models.View
			query := ctx.DB().Select("id").Model(&models.View{}).Where("deleted_at IS NULL")
			if err := query.Order("name ASC").Find(&views).Error; err != nil {
				return api.WriteError(c, ctx.Oops().Wrapf(err, "failed to list views for subject access search"))
			}

			for _, view := range views {
				r, err := runSubjectAccessReview(ctx, SubjectAccessReviewRequest{
					Subjects: []string{req.Subject},
					Action:   req.Action,
					Resource: SubjectAccessReviewResource{
						View: view.ID.String(),
					},
				})
				if err != nil {
					return api.WriteError(c, err)
				}

				for _, x := range r {
					if x.Allowed {
						results = append(results, SubjectAccessSearchResult{
							ResourceType: "view",
							ID:           view.ID.String(),
						})
					}
				}
			}
		}
	}

	return c.JSON(http.StatusOK, SubjectAccessSearchResponse{
		Subject:       req.Subject,
		Action:        req.Action,
		ResourceTypes: req.ResourceTypes,
		Total:         len(results),
		Results:       results,
	})
}

func Dump(c echo.Context) error {
	perms, err := rbac.Enforcer().GetPolicy()
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, policy.NewPermissions(perms))
}
