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
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/samber/lo"
	"gorm.io/gorm"
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

type SubjectAccessReviewResource struct {
	Playbook string `json:"playbook,omitempty"`
	Config   string `json:"config,omitempty"`
	Check    string `json:"check,omitempty"`
	View     string `json:"view,omitempty"`
	Global   string `json:"global,omitempty"`
}

type SubjectAccessReviewRequest struct {
	Resource SubjectAccessReviewResource `json:"resource"`
	Action   string                      `json:"action"`

	// Supports ["*"], in which case we iterate over all permission subjects in the database
	Subjects []string `json:"subjects"`
}

type SubjectAccessReviewResult struct {
	Subject string `json:"subject"`
	Allowed bool   `json:"allowed"`
	Error   string `json:"error,omitempty"`
}

func (req SubjectAccessReviewRequest) Validate(ctx context.Context) error {
	if req.Action == "" {
		return api.Errorf(api.EINVALID, "action is required")
	}

	resourceFields := 0
	if req.Resource.Global != "" {
		resourceFields++
	}
	if req.Resource.Playbook != "" {
		resourceFields++
	}
	if req.Resource.Config != "" {
		resourceFields++
	}
	if req.Resource.Check != "" {
		resourceFields++
	}
	if req.Resource.View != "" {
		resourceFields++
	}
	if resourceFields == 0 {
		return api.Errorf(api.EINVALID, "at least one of resource.global, resource.playbook, resource.config, resource.check or resource.view is required")
	}

	if !lo.Contains(policy.AllActions, req.Action) {
		return api.Errorf(api.EINVALID, "unsupported action %q, only %s are supported", req.Action, strings.Join(policy.AllActions, ", "))
	}

	if len(req.Subjects) == 0 {
		return api.Errorf(api.EINVALID, "at least one subject is required")
	}

	return nil
}

type SubjectAccessReviewResponse struct {
	Resource SubjectAccessReviewResource `json:"resource"`
	Action   string                      `json:"action"`
	Results  []SubjectAccessReviewResult `json:"results"`
}

const (
	defaultSubjectAccessSearchLimit = 100
	maxSubjectAccessSearchLimit     = 500
)

type SubjectAccessSearchRequest struct {
	Subject       string   `json:"subject"`
	Action        string   `json:"action"`
	ResourceTypes []string `json:"resource_types,omitempty"`
	Limit         int      `json:"limit,omitempty"`
	Offset        int      `json:"offset,omitempty"`
}

type SubjectAccessSearchResult struct {
	ResourceType string `json:"resource_type"`
	ID           string `json:"id"`
	Name         string `json:"name"`
	Namespace    string `json:"namespace,omitempty"`
}

type SubjectAccessSearchResponse struct {
	Subject       string                      `json:"subject"`
	Action        string                      `json:"action"`
	ResourceTypes []string                    `json:"resource_types"`
	Total         int                         `json:"total"`
	Limit         int                         `json:"limit"`
	Offset        int                         `json:"offset"`
	Results       []SubjectAccessSearchResult `json:"results"`
}

func (req *SubjectAccessSearchRequest) Validate() error {
	req.Subject = strings.TrimSpace(req.Subject)

	if req.Subject == "" {
		return api.Errorf(api.EINVALID, "subject is required")
	}

	if req.Action == "" {
		return api.Errorf(api.EINVALID, "action is required")
	}

	if !lo.Contains(policy.AllActions, req.Action) {
		return api.Errorf(api.EINVALID, "unsupported action %q, only %s are supported", req.Action, strings.Join(policy.AllActions, ", "))
	}

	if req.Limit <= 0 {
		req.Limit = defaultSubjectAccessSearchLimit
	}

	if req.Limit > maxSubjectAccessSearchLimit {
		return api.Errorf(api.EINVALID, "limit exceeds maximum of %d", maxSubjectAccessSearchLimit)
	}

	if req.Offset < 0 {
		return api.Errorf(api.EINVALID, "offset must be greater than or equal to 0")
	}

	if len(req.ResourceTypes) == 0 {
		req.ResourceTypes = []string{"playbook", "view"}
	}

	normalized := make([]string, 0, len(req.ResourceTypes))
	seen := map[string]struct{}{}
	for _, resourceType := range req.ResourceTypes {
		resourceType = strings.ToLower(strings.TrimSpace(resourceType))
		switch resourceType {
		case "playbook", "view":
			if _, ok := seen[resourceType]; !ok {
				normalized = append(normalized, resourceType)
				seen[resourceType] = struct{}{}
			}
		default:
			return api.Errorf(api.EINVALID, "unsupported resource_type %q, only playbook and view are supported", resourceType)
		}
	}

	req.ResourceTypes = normalized
	return nil
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
			query := ctx.DB().Model(&models.Playbook{}).Where("deleted_at IS NULL")
			if err := query.Order("COALESCE(title, name) ASC").Find(&playbooks).Error; err != nil {
				return api.WriteError(c, ctx.Oops().Wrapf(err, "failed to list playbooks for subject access search"))
			}

			for _, playbook := range playbooks {
				attr := &models.ABACAttribute{Playbook: playbook}
				if !rbac.HasPermission(ctx, req.Subject, attr, req.Action) {
					continue
				}

				results = append(results, SubjectAccessSearchResult{
					ResourceType: "playbook",
					ID:           playbook.ID.String(),
					Name:         playbook.Name,
					Namespace:    playbook.Namespace,
				})
			}
		case "view":
			var views []models.View
			query := ctx.DB().Model(&models.View{}).Where("deleted_at IS NULL")
			if err := query.Order("name ASC").Find(&views).Error; err != nil {
				return api.WriteError(c, ctx.Oops().Wrapf(err, "failed to list views for subject access search"))
			}

			for _, view := range views {
				attr := &models.ABACAttribute{View: view}
				if !rbac.HasPermission(ctx, req.Subject, attr, req.Action) {
					continue
				}

				results = append(results, SubjectAccessSearchResult{
					ResourceType: "view",
					ID:           view.ID.String(),
					Name:         view.Name,
					Namespace:    view.Namespace,
				})
			}
		}
	}

	total := len(results)
	start := req.Offset
	if start > total {
		start = total
	}
	end := start + req.Limit
	if end > total {
		end = total
	}

	return c.JSON(http.StatusOK, SubjectAccessSearchResponse{
		Subject:       req.Subject,
		Action:        req.Action,
		ResourceTypes: req.ResourceTypes,
		Total:         total,
		Limit:         req.Limit,
		Offset:        req.Offset,
		Results:       results[start:end],
	})
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

	subjects, err := resolveAccessReviewSubjects(ctx, req.Subjects)
	if err != nil {
		return err
	} else if len(subjects) > maxSubjectAccessReviewSubjects {
		return api.Errorf(api.EINVALID, "subjects exceeds maximum of %d", maxSubjectAccessReviewSubjects)
	}

	resourceAttr, err := resolveAccessReviewResource(ctx, req.Resource)
	if err != nil {
		return api.WriteError(c, err)
	}

	results := make([]SubjectAccessReviewResult, 0, len(subjects))
	for _, subject := range subjects {
		subject = strings.TrimSpace(subject)
		if subject == "" {
			results = append(results, SubjectAccessReviewResult{Subject: subject, Error: "subject is required"})
			continue
		}

		allowed := rbac.HasPermission(ctx, subject, resourceAttr, req.Action)
		if req.Resource.Global != "" {
			allowed = rbac.Check(ctx, subject, req.Resource.Global, req.Action)
		}

		results = append(results, SubjectAccessReviewResult{Subject: subject, Allowed: allowed})
	}

	return c.JSON(http.StatusOK, SubjectAccessReviewResponse{
		Resource: req.Resource,
		Action:   req.Action,
		Results:  results,
	})
}

func resolveAccessReviewSubjects(ctx context.Context, subjects []string) ([]string, error) {
	if len(subjects) != 1 || strings.TrimSpace(subjects[0]) != "*" {
		return subjects, nil
	}

	return db.GetPermissionSubjects(ctx)
}

func resolveAccessReviewResource(ctx context.Context, resource SubjectAccessReviewResource) (*models.ABACAttribute, error) {
	attr := &models.ABACAttribute{}

	if resource.Playbook != "" {
		playbookID, err := uuid.Parse(resource.Playbook)
		if err != nil {
			return nil, api.Errorf(api.EINVALID, "resource.playbook must be a valid UUID")
		}

		var playbook models.Playbook
		if err := ctx.DB().Where("id = ? AND deleted_at IS NULL", playbookID).First(&playbook).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return nil, api.Errorf(api.ENOTFOUND, "playbook %q not found", resource.Playbook)
			}
			return nil, ctx.Oops().Wrapf(err, "failed to resolve playbook %q", resource.Playbook)
		}

		attr.Playbook = playbook
	}

	if resource.Config != "" {
		configID, err := uuid.Parse(resource.Config)
		if err != nil {
			return nil, api.Errorf(api.EINVALID, "resource.config must be a valid UUID")
		}

		var config models.ConfigItem
		if err := ctx.DB().Where("id = ? AND deleted_at IS NULL", configID).First(&config).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return nil, api.Errorf(api.ENOTFOUND, "config %q not found", resource.Config)
			}
			return nil, ctx.Oops().Wrapf(err, "failed to resolve config %q", resource.Config)
		}

		attr.Config = config
	}

	if resource.Check != "" {
		checkID, err := uuid.Parse(resource.Check)
		if err != nil {
			return nil, api.Errorf(api.EINVALID, "resource.check must be a valid UUID")
		}

		var check models.Check
		if err := ctx.DB().Where("id = ? AND deleted_at IS NULL", checkID).First(&check).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return nil, api.Errorf(api.ENOTFOUND, "check %q not found", resource.Check)
			}
			return nil, ctx.Oops().Wrapf(err, "failed to resolve check %q", resource.Check)
		}

		attr.Check = check
	}

	if resource.View != "" {
		viewID, err := uuid.Parse(resource.View)
		if err != nil {
			return nil, api.Errorf(api.EINVALID, "resource.view must be a valid UUID")
		}

		var view models.View
		if err := ctx.DB().Where("id = ? AND deleted_at IS NULL", viewID).First(&view).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return nil, api.Errorf(api.ENOTFOUND, "view %q not found", resource.View)
			}
			return nil, ctx.Oops().Wrapf(err, "failed to resolve view %q", resource.View)
		}

		attr.View = view
	}

	return attr, nil
}

func Dump(c echo.Context) error {
	perms, err := rbac.Enforcer().GetPolicy()
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, policy.NewPermissions(perms))
}
