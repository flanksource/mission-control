package db

import (
	"encoding/json"
	"fmt"

	"github.com/flanksource/duty"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/plugin/registry"
)

// PluginFromCRD converts a v1.Plugin CRD into the persisted models.Plugin
// row. The full PluginSpec is JSON-encoded into the spec jsonb column;
// pluginToSpec reverses this when rehydrating the registry on startup.
func PluginFromCRD(obj *v1.Plugin) (models.Plugin, error) {
	dbObj := models.Plugin{
		Name:          obj.Name,
		Namespace:     obj.Namespace,
		Source:        models.SourceCRD,
		InstalledPath: obj.Status.InstalledPath,
		PluginVersion: obj.Status.PluginVersion,
	}

	if obj.GetUID() != "" {
		uid, err := uuid.Parse(string(obj.GetUID()))
		if err != nil {
			return dbObj, fmt.Errorf("failed to parse uid: %w", err)
		}
		dbObj.ID = uid
	}

	specJSON, err := json.Marshal(obj.Spec)
	if err != nil {
		return dbObj, fmt.Errorf("marshal spec: %w", err)
	}
	dbObj.Spec = types.JSON(specJSON)

	return dbObj, nil
}

// pluginToSpec is the reverse of PluginFromCRD: it reconstructs a
// v1.PluginSpec from the stored jsonb column so cold-start replay can hand
// it to the registry helper.
func pluginToSpec(row models.Plugin) (v1.PluginSpec, error) {
	var spec v1.PluginSpec
	if len(row.Spec) == 0 {
		return spec, nil
	}
	if err := json.Unmarshal(row.Spec, &spec); err != nil {
		return spec, fmt.Errorf("unmarshal spec: %w", err)
	}
	return spec, nil
}

// PersistPluginFromCRD is the kopper persist callback. It upserts the
// plugins table row, then asks the registry to install the binary and start
// the supervisor.
func PersistPluginFromCRD(ctx context.Context, obj *v1.Plugin) error {
	if obj == nil {
		return ctx.Oops().Errorf("nil Plugin")
	}

	dbObj, err := PluginFromCRD(obj)
	if err != nil {
		return ctx.Oops().Wrap(err)
	}

	if err := ctx.DB().Save(&dbObj).Error; err != nil {
		return ctx.Oops().Wrapf(err, "failed to persist plugin %s/%s", obj.Namespace, obj.Name)
	}

	binPath, err := registry.ApplyToRegistry(ctx, obj.Name, obj.Spec)
	if err != nil {
		return ctx.Oops().Wrap(err)
	}

	if binPath != "" && binPath != dbObj.InstalledPath {
		if err := ctx.DB().Model(&models.Plugin{}).
			Where("id = ?", dbObj.ID).
			Update("installed_path", binPath).Error; err != nil {
			ctx.Logger.V(2).Infof("plugin %s: failed to write installed_path: %v", obj.Name, err)
		}
	}

	return nil
}

// DeletePlugin is the kopper delete callback. It soft-deletes the row, then
// stops the supervised process and drops the registry entry.
func DeletePlugin(ctx context.Context, id string) error {
	var row models.Plugin
	if err := ctx.DB().Where("id = ?", id).First(&row).Error; err == nil {
		registry.RemoveFromRegistry(row.Name)
	} else {
		ctx.Logger.V(3).Infof("delete plugin %s: no DB row to look up name from: %v", id, err)
	}

	return ctx.DB().Model(&models.Plugin{}).
		Where("id = ? AND deleted_at IS NULL", id).
		Update("deleted_at", duty.Now()).Error
}

// DeleteStalePlugin soft-deletes prior rows that share the new CRD's
// (name, namespace) but have a different UID — the result of a CRD being
// renamed or recreated.
func DeleteStalePlugin(ctx context.Context, newer *v1.Plugin) error {
	if newer == nil {
		return nil
	}
	registry.StaleFromRegistry(newer)
	return ctx.DB().Model(&models.Plugin{}).
		Where("name = ? AND namespace = ?", newer.Name, newer.Namespace).
		Where("id != ?", newer.UID).
		Where("deleted_at IS NULL").
		Update("deleted_at", duty.Now()).Error
}

// ListPlugins returns every non-deleted plugin row.
func ListPlugins(ctx context.Context) ([]models.Plugin, error) {
	var rows []models.Plugin
	if err := ctx.DB().Where("deleted_at IS NULL").Find(&rows).Error; err != nil {
		return nil, ctx.Oops().Wrapf(err, "list plugins")
	}
	return rows, nil
}

// ReplayPlugins rehydrates the in-memory registry from the plugins table.
// Called once at boot, after WireSupervisor and before mgr.Start, so
// pre-existing plugins are running before kopper events arrive.
func ReplayPlugins(ctx context.Context) error {
	rows, err := ListPlugins(ctx)
	if err != nil {
		return err
	}

	for _, row := range rows {
		spec, err := pluginToSpec(row)
		if err != nil {
			ctx.Logger.Errorf("plugin %s: replay decode failed: %v", row.Name, err)
			continue
		}
		if _, err := registry.ApplyToRegistry(ctx, row.Name, spec); err != nil {
			ctx.Logger.Errorf("plugin %s: replay apply failed: %v", row.Name, err)
		}
	}
	return nil
}
