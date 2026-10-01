package db

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/flanksource/duty"
	"github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/google/uuid"

	v1 "github.com/flanksource/incident-commander/api/v1"
)

func PersistPermissionFromCRD(ctx context.Context, obj *v1.Permission) error {
	uid, err := uuid.Parse(string(obj.GetUID()))
	if err != nil {
		return err
	}

	subject, subjectType, err := obj.Spec.Subject.Populate(ctx)
	if err != nil {
		return ctx.Oops(api.EINVALID).Wrapf(err, "failed to populate subject")
	}

	// Check for deprecated fields and emit warning
	if len(obj.Spec.Tags) > 0 || len(obj.Spec.Agents) > 0 {
		ctx.Warnf("Permission %s/%s uses deprecated fields (tags or agents). These fields are ignored. Use Scope CRD instead.",
			obj.Namespace, obj.Name)
	}

	if err := obj.Spec.Object.Validate(); err != nil {
		return ctx.Oops(api.EINVALID).Wrapf(err, "invalid permission object")
	}

	action := strings.Join(obj.Spec.Actions, ",")

	p := models.Permission{
		ID:          uid,
		Name:        obj.GetName(),
		Subject:     subject,
		SubjectType: subjectType,
		Namespace:   obj.GetNamespace(),
		Description: obj.Spec.Description,
		Action:      action,
		Source:      models.SourceCRD,
		Deny:        obj.Spec.Deny,
	}

	if err := setPermissionObject(&p, obj.Spec.Object); err != nil {
		return ctx.Oops(api.EINTERNAL).Wrap(err)
	}

	return ctx.DB().Save(&p).Error
}

// setPermissionObject sets the object of a permission, using the global object
// when the selectors semantically match one.
func setPermissionObject(p *models.Permission, object v1.PermissionObject) error {
	if globalObject, ok := object.GlobalObject(); ok {
		p.Object = globalObject
		return nil
	}

	selectors, err := json.Marshal(object)
	if err != nil {
		return fmt.Errorf("failed to marshal object: %w", err)
	}
	p.ObjectSelector = selectors
	return nil
}

func PersistPermissionGroupFromCRD(ctx context.Context, obj *v1.PermissionGroup) error {
	uid, err := uuid.Parse(string(obj.GetUID()))
	if err != nil {
		return err
	}

	ctx.Warnf("PermissionGroup %s/%s is deprecated. Use Role and RoleBinding instead",
		obj.Namespace, obj.Name)

	selectors, err := json.Marshal(obj.Spec.PermissionGroupSubjects)
	if err != nil {
		return err
	}

	group := models.PermissionGroup{
		ID:        uid,
		Name:      obj.GetName(),
		Namespace: obj.GetNamespace(),
		Source:    models.SourceCRD,
		Selectors: selectors,
	}

	return ctx.DB().Save(&group).Error
}

func DeletePermission(ctx context.Context, id string) error {
	return ctx.DB().Model(&models.Permission{}).Where("id = ?", id).Update("deleted_at", duty.Now()).Error
}

func DeleteStalePermission(ctx context.Context, newer *v1.Permission) error {
	return ctx.DB().Model(&models.Permission{}).
		Where("name = ? AND namespace = ?", newer.Name, newer.Namespace).
		Where("id != ?", newer.UID).
		Where("deleted_at IS NULL").
		Update("deleted_at", duty.Now()).Error
}

func DeletePermissionGroup(ctx context.Context, id string) error {
	return ctx.DB().Model(&models.PermissionGroup{}).Where("id = ?", id).Update("deleted_at", duty.Now()).Error
}

func DeleteStalePermissionGroup(ctx context.Context, newer *v1.PermissionGroup) error {
	return ctx.DB().Model(&models.PermissionGroup{}).
		Where("namespace = ? AND name = ?", newer.Namespace, newer.Name).
		Where("deleted_at IS NULL").
		Update("deleted_at", duty.Now()).Error
}
