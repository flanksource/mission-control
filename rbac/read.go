package rbac

import (
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
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
