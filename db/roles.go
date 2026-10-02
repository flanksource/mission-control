package db

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/flanksource/duty"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	"github.com/flanksource/kopper"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/rbac/adapter"
)

// validity returns the error and reason to store for an object, from its validation error.
// Errors other than validation errors are returned, and nothing is stored.
func validity(err error) (message, reason *string, _ error) {
	if err == nil {
		return nil, nil, nil
	} else if !adapter.IsValidationError(err) {
		return nil, nil, err
	}
	return lo.ToPtr(err.Error()), lo.ToPtr(adapter.InvalidReason(err)), nil
}

// notReadyRetry is when an object that isn't in effect is validated again. Changes to the Scopes, Roles and
// RoleBindings it references reconcile it right away; this catches the rest, e.g. an agent registered again.
const notReadyRetry = 5 * time.Minute

// notReady reports a stored object that isn't in effect, so its status is Ready=False with the reason.
func notReady(reason, message *string) error {
	if message == nil {
		return nil
	}
	return &kopper.NotReadyError{Reason: lo.FromPtr(reason), Message: lo.FromPtr(message), RequeueAfter: notReadyRetry}
}

// PersistRoleFromCRD stores a Role CRD. See PersistRole.
func PersistRoleFromCRD(ctx context.Context, obj *v1.Role) error {
	return PersistRole(ctx, obj, models.SourceCRD, nil)
}

// PersistRole stores the role as written, with why it isn't in effect when it's invalid.
// An invalid role is stored too: it applies none of its rules until it becomes valid.
// Its status lists the bindings that some of its allow rules don't apply through.
func PersistRole(ctx context.Context, obj *v1.Role, source string, createdBy *uuid.UUID) error {
	uid, err := uuid.Parse(string(obj.GetUID()))
	if err != nil {
		return err
	}

	rules, err := json.Marshal(obj.Spec.Rules)
	if err != nil {
		return err
	}

	role := models.Role{
		ID:          uid,
		Name:        obj.GetName(),
		Namespace:   obj.GetNamespace(),
		Description: obj.Spec.Description,
		Source:      source,
		Rules:       rules,
		CreatedBy:   createdBy,
	}

	_, validationErr := adapter.ValidateRole(ctx, nil, role)
	if role.Error, role.ErrorReason, err = validity(validationErr); err != nil {
		return err
	}

	// Returned unwrapped so kopper can detect unique constraint violations and delete the stale role
	if err := ctx.DB().Save(&role).Error; err != nil {
		return err
	}

	if obj.Status.BindingsWithUnappliedRules, err = adapter.BindingsWithUnappliedRules(ctx, role); err != nil {
		return err
	}

	return notReady(role.ErrorReason, role.Error)
}

func DeleteRole(ctx context.Context, id string) error {
	return ctx.DB().Model(&models.Role{}).Where("id = ?", id).Update("deleted_at", duty.Now()).Error
}

// DeleteStaleRole replaces an older role of the same name with the newer one.
func DeleteStaleRole(ctx context.Context, newer *v1.Role) error {
	if err := ctx.DB().Model(&models.Role{}).
		Where("namespace = ? AND name = ?", newer.Namespace, newer.Name).
		Where("id != ?", newer.UID).
		Where("deleted_at IS NULL").
		Update("deleted_at", duty.Now()).Error; err != nil {
		return err
	}

	return PersistRoleFromCRD(ctx, newer)
}

// PersistRoleBindingFromCRD stores a RoleBinding CRD. See PersistRoleBinding.
func PersistRoleBindingFromCRD(ctx context.Context, obj *v1.RoleBinding) error {
	return PersistRoleBinding(ctx, obj, models.SourceCRD, nil)
}

// PersistRoleBinding stores the binding as written, with why it isn't Ready: its role doesn't exist or is invalid,
// a Scope of its constraint doesn't exist or is invalid, or none of the role's allow rules applies through it.
// Its AllRulesApply condition reports the allow rules its constraint can't narrow.
func PersistRoleBinding(ctx context.Context, obj *v1.RoleBinding, source string, createdBy *uuid.UUID) error {
	uid, err := uuid.Parse(string(obj.GetUID()))
	if err != nil {
		return err
	}

	subjects, err := json.Marshal(obj.Spec.Subjects)
	if err != nil {
		return err
	}

	var constraint *types.JSON
	if c := obj.Spec.Constraint; c != nil {
		// An empty constraint would be stored as NULL, i.e. no constraint, and grant the role un-narrowed.
		if c.Resource == nil && c.Target == nil {
			return fmt.Errorf("role binding %s/%s: constraint must set resource or target", obj.GetNamespace(), obj.GetName())
		}

		raw, err := json.Marshal(c)
		if err != nil {
			return err
		}
		constraint = lo.ToPtr(types.JSON(raw))
	}

	binding := models.RoleBinding{
		ID:          uid,
		Name:        obj.GetName(),
		Namespace:   obj.GetNamespace(),
		Description: obj.Spec.Description,
		Source:      source,
		Role:        obj.Spec.Role,
		Constraint:  constraint,
		Subjects:    subjects,
		CreatedBy:   createdBy,
	}

	compiled, validationErr := adapter.ValidateBinding(ctx, nil, binding)
	if binding.Error, binding.ErrorReason, err = validity(validationErr); err != nil {
		return err
	}

	if err := ctx.DB().Save(&binding).Error; err != nil {
		return err
	}

	setAllRulesApply(obj, compiled.Unapplied, binding.ErrorReason)
	return notReady(binding.ErrorReason, binding.Error)
}

// setAllRulesApply sets the AllRulesApply condition of a binding: whether every allow rule of its role applies
// through it. It's Unknown when that can't be told, because the role or a Scope of the constraint is missing or invalid.
func setAllRulesApply(obj *v1.RoleBinding, unapplied []adapter.UnappliedRule, notReadyReason *string) {
	condition := metav1.Condition{
		Type:               v1.ConditionAllRulesApply,
		Status:             metav1.ConditionTrue,
		Reason:             "AllRulesApply",
		ObservedGeneration: obj.GetGeneration(),
	}

	if len(unapplied) > 0 {
		reasons := lo.Uniq(lo.Map(unapplied, func(u adapter.UnappliedRule, _ int) string { return u.Reason }))
		condition.Status = metav1.ConditionFalse
		condition.Reason = lo.Ternary(len(reasons) == 1, reasons[0], adapter.ReasonConstraintDoesNotFit)
		condition.Message = strings.Join(lo.Map(unapplied, func(u adapter.UnappliedRule, _ int) string { return u.String() }), "; ")
	} else if notReadyReason != nil {
		condition.Status = metav1.ConditionUnknown
		condition.Reason = *notReadyReason
	}

	meta.SetStatusCondition(&obj.Status.Conditions, condition)
}

func DeleteRoleBinding(ctx context.Context, id string) error {
	return ctx.DB().Model(&models.RoleBinding{}).Where("id = ?", id).Update("deleted_at", duty.Now()).Error
}

// DeleteStaleRoleBinding replaces an older binding of the same name with the newer one.
func DeleteStaleRoleBinding(ctx context.Context, newer *v1.RoleBinding) error {
	if err := ctx.DB().Model(&models.RoleBinding{}).
		Where("namespace = ? AND name = ?", newer.Namespace, newer.Name).
		Where("id != ?", newer.UID).
		Where("deleted_at IS NULL").
		Update("deleted_at", duty.Now()).Error; err != nil {
		return err
	}

	return PersistRoleBindingFromCRD(ctx, newer)
}
