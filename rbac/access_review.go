package rbac

import (
	"strings"

	"github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/incident-commander/db"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"gorm.io/gorm"
)

type SubjectAccessSearchRequest struct {
	Subject       string   `json:"subject"`
	Action        string   `json:"action"`
	ResourceTypes []string `json:"resource_types,omitempty"`
}

type SubjectAccessSearchResult struct {
	ResourceType string `json:"resource_type"`
	ID           string `json:"id"`
}

type SubjectAccessSearchResponse struct {
	Subject       string                      `json:"subject"`
	Action        string                      `json:"action"`
	ResourceTypes []string                    `json:"resource_types"`
	Total         int                         `json:"total"`
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

	if len(req.ResourceTypes) == 0 {
		req.ResourceTypes = []string{"playbook", "view"}
	}

	normalized := make([]string, 0, len(req.ResourceTypes))
	seen := map[string]struct{}{}
	for _, resourceType := range req.ResourceTypes {
		resourceType = strings.ToLower(strings.TrimSpace(resourceType))
		switch resourceType {
		case "playbook", "view", "connection":
			if _, ok := seen[resourceType]; !ok {
				normalized = append(normalized, resourceType)
				seen[resourceType] = struct{}{}
			}
		default:
			return api.Errorf(api.EINVALID, "unsupported resource_type %q, only playbook, view and connection are supported", resourceType)
		}
	}

	req.ResourceTypes = normalized
	return nil
}

type SubjectAccessReviewResource struct {
	Playbook   string `json:"playbook,omitempty"`
	Config     string `json:"config,omitempty"`
	Check      string `json:"check,omitempty"`
	View       string `json:"view,omitempty"`
	Connection string `json:"connection,omitempty"`
	Global     string `json:"global,omitempty"`
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
	if req.Resource.Connection != "" {
		resourceFields++
	}
	if resourceFields == 0 {
		return api.Errorf(api.EINVALID, "at least one of resource.global, resource.playbook, resource.config, resource.check, resource.view or resource.connection is required")
	}

	if !lo.Contains(policy.AllActions, req.Action) {
		return api.Errorf(api.EINVALID, "unsupported action %q, only %s are supported", req.Action, strings.Join(policy.AllActions, ", "))
	}

	if len(req.Subjects) == 0 {
		return api.Errorf(api.EINVALID, "at least one subject is required")
	}

	return nil
}

func runSubjectAccessReview(ctx context.Context, req SubjectAccessReviewRequest) ([]SubjectAccessReviewResult, error) {
	subjects, err := resolveAccessReviewSubjects(ctx, req.Subjects)
	if err != nil {
		return nil, err
	} else if len(subjects) > maxSubjectAccessReviewSubjects {
		return nil, api.Errorf(api.EINVALID, "subjects exceeds maximum of %d", maxSubjectAccessReviewSubjects)
	}

	resourceAttr, err := resolveAccessReviewResource(ctx, req.Resource)
	if err != nil {
		return nil, err
	}

	results := make([]SubjectAccessReviewResult, 0, len(subjects))
	for _, subject := range subjects {
		subject = strings.TrimSpace(subject)
		if subject == "" {
			results = append(results, SubjectAccessReviewResult{Subject: subject, Error: "subject is required"})
			continue
		}

		var allowed bool
		if req.Resource.Global != "" {
			allowed = rbac.Check(ctx, subject, req.Resource.Global, req.Action)
		} else {
			allowed = rbac.HasPermission(ctx, subject, resourceAttr, req.Action)
		}

		results = append(results, SubjectAccessReviewResult{Subject: subject, Allowed: allowed})
	}

	return results, nil
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

	if resource.Connection != "" {
		connectionID, err := uuid.Parse(resource.Connection)
		if err != nil {
			return nil, api.Errorf(api.EINVALID, "resource.connection must be a valid UUID")
		}

		var connection models.Connection
		if err := ctx.DB().Where("id = ? AND deleted_at IS NULL", connectionID).First(&connection).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return nil, api.Errorf(api.ENOTFOUND, "connection %q not found", resource.Connection)
			}
			return nil, ctx.Oops().Wrapf(err, "failed to resolve connection %q", resource.Connection)
		}

		attr.Connection = connection
	}

	return attr, nil
}
