package rbac

import (
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/membership"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/google/uuid"
)

// CanRead reports whether the subject may read the resource in attr.
//
// A check is readable through its own Scopes or through its canary's, as row-level security lists it.
func CanRead(ctx context.Context, subject string, attr *models.ABACAttribute) bool {
	if rbac.HasPermission(ctx, subject, attr, policy.ActionRead) {
		return true
	}

	if attr == nil || attr.Check.ID == uuid.Nil || attr.Check.CanaryID == uuid.Nil {
		return false
	}

	var canaries []models.Canary
	if err := ctx.DB().Where("id = ?", attr.Check.CanaryID).Limit(1).Find(&canaries).Error; err != nil {
		ctx.Errorf("failed to get canary %s of check %s: %v", attr.Check.CanaryID, attr.Check.ID, err)
		return false
	} else if len(canaries) == 0 {
		return false
	}

	return rbac.HasPermission(ctx, subject, &models.ABACAttribute{Canary: canaries[0]}, policy.ActionRead)
}

// ForOperation reads the Scope memberships of every resource an operation involves, a check's canary included,
// and returns a context whose checks all use them, so they see the membership of one moment.
// Call it once, before the operation's checks.
func ForOperation(ctx context.Context, attrs ...*models.ABACAttribute) (context.Context, error) {
	refs := rbac.MembershipRefs(attrs...)
	for _, attr := range attrs {
		if attr != nil && attr.Check.ID != uuid.Nil && attr.Check.CanaryID != uuid.Nil {
			refs = append(refs, membership.Ref{Type: policy.ResourceCanary, ID: attr.Check.CanaryID})
		}
	}

	return membership.ForOperation(ctx, refs...)
}
