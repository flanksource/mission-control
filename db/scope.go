package db

import (
	"encoding/json"

	"github.com/flanksource/duty"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"

	v1 "github.com/flanksource/incident-commander/api/v1"
)

// PersistScopeFromCRD saves a Scope CRD to the database
func PersistScopeFromCRD(ctx context.Context, obj *v1.Scope) error {
	uid, err := uuid.Parse(string(obj.GetUID()))
	if err != nil {
		return ctx.Oops().Wrapf(err, "failed to parse UID")
	}

	// Resolve agent names to IDs in targets before saving
	for i := range obj.Spec.Targets {
		if err := resolveAgentInTarget(ctx, &obj.Spec.Targets[i]); err != nil {
			return err
		}
	}

	// Marshal targets to JSON for storage
	targetsJSON, err := json.Marshal(obj.Spec.Targets)
	if err != nil {
		return ctx.Oops().Wrapf(err, "failed to marshal targets")
	}

	scope := models.Scope{
		ID:          uid,
		Name:        obj.GetName(),
		Namespace:   obj.GetNamespace(),
		Description: obj.Spec.Description,
		Targets:     types.JSON(targetsJSON),
		Source:      models.SourceCRD,
	}

	return ctx.DB().Save(&scope).Error
}

// resolveAgentInTarget resolves agent names to agent IDs in a scope target
func resolveAgentInTarget(ctx context.Context, target *v1.ScopeTarget) error {
	selectors := []*v1.ScopeResourceSelector{
		target.Config,
		target.Component,
		target.Playbook,
		target.Canary,
	}

	for _, selector := range selectors {
		if selector != nil && selector.Agent != "" {
			if _, err := uuid.Parse(selector.Agent); err == nil {
				// Already a UUID, no need to resolve
				continue
			}

			var agent models.Agent
			if err := ctx.DB().Where("deleted_at IS NULL").Where("name = ?", selector.Agent).First(&agent).Error; err != nil {
				return ctx.Oops().Wrapf(err, "failed to resolve agent name %q", selector.Agent)
			}

			selector.Agent = agent.ID.String()
		}
	}

	return nil
}

// DeleteScope soft deletes a Scope by ID
func DeleteScope(ctx context.Context, id string) error {
	return ctx.DB().Model(&models.Scope{}).
		Where("id = ?", id).
		Update("deleted_at", duty.Now()).Error
}

// DeleteStaleScope soft deletes old Scope resources with the same name/namespace
func DeleteStaleScope(ctx context.Context, newer *v1.Scope) error {
	return ctx.DB().Model(&models.Scope{}).
		Where("name = ? AND namespace = ?", newer.Name, newer.Namespace).
		Where("id != ?", newer.UID).
		Where("deleted_at IS NULL").
		Update("deleted_at", duty.Now()).Error
}
