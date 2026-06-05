package reconciler

import (
	"fmt"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/query/grammar"
	"github.com/google/uuid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/plugin"
	"github.com/flanksource/incident-commander/plugin/machinery"
	"github.com/flanksource/incident-commander/plugin/manifestcache"
)

func PersistPluginFromCRD(ctx context.Context, p *v1.Plugin) error {
	if p == nil {
		return fmt.Errorf("nil Plugin")
	}
	if p.Spec.Source == "" {
		return fmt.Errorf("plugin %s: spec.source is required", p.Name)
	}
	if err := validatePluginSelector(p); err != nil {
		return err
	}

	id, err := parsePluginID(string(p.UID))
	if err != nil {
		return err
	}

	if err := db.PersistPluginFromCRD(ctx, p); err != nil {
		return err
	}

	var previous *plugin.Entry
	if entry := plugin.DefaultRegistry.Get(id); entry != nil {
		copy := *entry
		previous = &copy
	}

	if _, err := plugin.DefaultRegistry.Upsert(id, p.Namespace, p.Name, p.Spec); err != nil {
		return err
	}

	binaryChanged := previous != nil && (previous.Spec.Source != p.Spec.Source || previous.Spec.Version != p.Spec.Version)
	if binaryChanged {
		if err := machinery.StopPlugin(id); err != nil {
			return err
		}
	}

	if err := machinery.StartPlugin(ctx, id); err != nil {
		if previous != nil {
			if _, rollbackErr := plugin.DefaultRegistry.Upsert(previous.ID, previous.Namespace, previous.Name, previous.Spec); rollbackErr != nil {
				ctx.Logger.Errorf("plugin %s: rollback registry entry: %v", id, rollbackErr)
			} else if binaryChanged {
				if restartErr := machinery.StartPlugin(ctx, previous.ID); restartErr != nil {
					ctx.Logger.Errorf("plugin %s: restart previous version after rollback: %v", id, restartErr)
				}
			}
		} else {
			plugin.DefaultRegistry.Remove(id)
		}
		return err
	}
	if entry := plugin.DefaultRegistry.Get(id); entry != nil && entry.Manifest != nil {
		p.Status.PluginVersion = entry.Manifest.Version
		p.Status.InstalledPath = entry.InstalledPath
	}
	if err := db.UpdatePluginStatus(ctx, id, p.Status); err != nil {
		ctx.Logger.V(2).Infof("plugin %s: failed to update persisted status: %v", p.Name, err)
	}

	return nil
}

// DeletePlugin is the kopper delete callback. It stops the supervised
// process and drops the registry entry. The binary is left on disk —
// re-creating the CRD with the same version won't re-download it.
func DeletePlugin(ctx context.Context, id string) error {
	pluginID, err := parsePluginID(id)
	if err != nil {
		entry, resolveErr := plugin.DefaultRegistry.Resolve(id)
		if resolveErr != nil {
			return resolveErr
		}
		if entry == nil {
			return err
		}
		pluginID = entry.ID
	}

	entry := plugin.DefaultRegistry.Get(pluginID)
	if entry == nil {
		resolved, err := plugin.DefaultRegistry.Resolve(id)
		if err != nil {
			return err
		}
		if resolved != nil {
			entry = resolved
			pluginID = resolved.ID
		}
	}

	if err := machinery.StopPlugin(pluginID); err != nil {
		return err
	}
	plugin.DefaultRegistry.Remove(pluginID)
	if entry != nil && entry.Name != "" {
		if err := manifestcache.Delete(entry.Name); err != nil {
			ctx.Logger.V(2).Infof("plugin %s: drop manifest cache: %v", entry.Name, err)
		}
	}
	return db.DeletePlugin(ctx, id)
}

// DeleteStalePlugin removes registry entries for plugins whose CRD has been
// renamed or replaced.
func DeleteStalePlugin(ctx context.Context, newer *v1.Plugin) error {
	if newer == nil {
		return nil
	}
	newerID, err := parsePluginID(string(newer.UID))
	if err != nil {
		return err
	}
	for _, e := range plugin.DefaultRegistry.List() {
		if e.Name != newer.Name || e.Namespace != newer.Namespace {
			continue
		}
		if e.ID == newerID && e.Spec.Source == newer.Spec.Source && e.Spec.Version == newer.Spec.Version {
			continue
		}
		if err := machinery.StopPlugin(e.ID); err != nil {
			return err
		}
		plugin.DefaultRegistry.Remove(e.ID)
		if err := manifestcache.Delete(e.Name); err != nil {
			ctx.Logger.V(2).Infof("plugin %s: drop manifest cache: %v", e.Name, err)
		}
	}
	return db.DeleteStalePlugin(ctx, newer)
}

// ReplayPlugins rehydrates the in-memory registry from persisted plugin rows.
func ReplayPlugins(ctx context.Context) error {
	rows, err := db.ListPlugins(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		spec, err := db.PluginToSpec(row)
		if err != nil {
			ctx.Logger.Errorf("plugin %s: replay decode failed: %v", row.Name, err)
			continue
		}
		obj := &v1.Plugin{
			ObjectMeta: metav1.ObjectMeta{
				Name:      row.Name,
				Namespace: row.Namespace,
				UID:       k8stypes.UID(row.ID.String()),
			},
			Spec: spec,
			Status: v1.PluginStatus{
				InstalledPath: row.InstalledPath,
				PluginVersion: row.PluginVersion,
			},
		}
		if err := PersistPluginFromCRD(ctx, obj); err != nil {
			ctx.Logger.Errorf("plugin %s: replay apply failed: %v", row.Name, err)
		}
	}
	return nil
}

func validatePluginSelector(p *v1.Plugin) error {
	selector := p.Spec.Selector
	if selector.IsEmpty() {
		return nil
	}

	peg, err := selector.ToPeg(true)
	if err != nil {
		return fmt.Errorf("plugin %s: invalid spec.selector: %w", p.Name, err)
	}
	if peg == "" {
		return nil
	}
	if _, err := grammar.ParsePEG(peg); err != nil {
		return fmt.Errorf("plugin %s: invalid spec.selector: %w", p.Name, err)
	}

	return nil
}

func parsePluginID(id string) (uuid.UUID, error) {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("plugin id %q is not a UUID: %w", id, err)
	}
	return parsed, nil
}
