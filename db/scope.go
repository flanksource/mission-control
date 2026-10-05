package db

import (
	"encoding/json"

	"github.com/flanksource/duty"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac/membership"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/rbac/adapter"
)

// PersistScopeFromCRD stores a Scope CRD. See PersistScope.
func PersistScopeFromCRD(ctx context.Context, obj *v1.Scope) error {
	return PersistScope(ctx, obj, models.SourceCRD, nil)
}

// PersistScope stores the Scope as written, with why it isn't in effect when it's invalid, and in the same transaction
// rewrites its membership: a valid Scope's targets and members; nothing for an invalid one. If the membership can't be
// rewritten, the save fails and the previous version of the Scope stays.
// Agents are stored as written, and resolved every time the Scope is validated.
// A Scope is never validated against the Roles and RoleBindings that reference it.
func PersistScope(ctx context.Context, obj *v1.Scope, source string, createdBy *uuid.UUID) error {
	uid, err := uuid.Parse(string(obj.GetUID()))
	if err != nil {
		return ctx.Oops().Wrapf(err, "failed to parse UID")
	}

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
		Source:      source,
		CreatedBy:   createdBy,
	}

	resolved, validationErr := adapter.ValidateScope(ctx, obj.Spec.Targets)
	if scope.Error, scope.ErrorReason, err = validity(validationErr); err != nil {
		return err
	}

	err = ctx.Transaction(func(txCtx context.Context, _ trace.Span) error {
		// Returned unwrapped so kopper can detect unique constraint violations and delete the stale scope
		if err := txCtx.DB().Save(&scope).Error; err != nil {
			return err
		}

		if err := adapter.SaveScopeMembership(txCtx, scope, resolved, validationErr); err != nil {
			return ctx.Oops().Wrap(err)
		}
		return nil
	})
	if err != nil {
		return err
	}

	return notReady(scope.ErrorReason, scope.Error)
}

// DeleteScope soft deletes a Scope by ID, with its membership. Roles and RoleBindings that reference it stop granting
// until they're updated.
func DeleteScope(ctx context.Context, id string) error {
	scopeID, err := uuid.Parse(id)
	if err != nil {
		return ctx.Oops().Wrapf(err, "invalid scope id %q", id)
	}

	return ctx.Transaction(func(txCtx context.Context, _ trace.Span) error {
		if err := txCtx.DB().Model(&models.Scope{}).Where("id = ?", id).Update("deleted_at", duty.Now()).Error; err != nil {
			return err
		}
		return membership.Clear(txCtx, scopeID)
	})
}

// DeleteStaleScope replaces an older Scope of the same name with the newer one.
func DeleteStaleScope(ctx context.Context, newer *v1.Scope) error {
	err := ctx.Transaction(func(txCtx context.Context, _ trace.Span) error {
		var stale []uuid.UUID
		if err := txCtx.DB().Raw(`UPDATE scopes SET deleted_at = NOW()
			WHERE name = ? AND namespace = ? AND id != ? AND deleted_at IS NULL
			RETURNING id`, newer.Name, newer.Namespace, newer.UID).Scan(&stale).Error; err != nil {
			return err
		}

		// At most one: a namespace and name are unique among Scopes that aren't deleted
		for _, id := range stale {
			if err := membership.Clear(txCtx, id); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	return PersistScopeFromCRD(ctx, newer)
}
