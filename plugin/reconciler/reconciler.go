package reconciler

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/flanksource/deps"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/query/grammar"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/plugin"
	"github.com/flanksource/incident-commander/plugin/machinery"
	"github.com/flanksource/incident-commander/plugin/machinery/local"
	"github.com/google/uuid"
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

	binDir := local.PluginPath()
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return fmt.Errorf("create plugin dir %s: %w", binDir, err)
	}

	binPath := filepath.Join(binDir, p.Name)
	if info, err := os.Stat(binPath); err == nil && !info.IsDir() {
		ctx.Logger.V(3).Infof("plugin %s: using existing binary at %s, skipping install", p.Name, binPath)
	} else {
		res, err := deps.InstallWithContext(ctx,
			p.Spec.Source,
			p.Spec.Version,
			deps.WithBinDir(binDir),
		)
		if err != nil {
			return fmt.Errorf("install plugin %s: %w", p.Name, err)
		}
		if res != nil && res.Error != nil {
			return fmt.Errorf("install plugin %s: %w", p.Name, res.Error)
		}
	}

	p.Status.InstalledPath = binPath

	id, err := parsePluginID(string(p.UID))
	if err != nil {
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

	if err := machinery.StartPlugin(ctx, id); err != nil {
		if previous != nil {
			if _, rollbackErr := plugin.DefaultRegistry.Upsert(previous.ID, previous.Namespace, previous.Name, previous.Spec); rollbackErr != nil {
				ctx.Logger.Errorf("plugin %s: rollback registry entry: %v", id, rollbackErr)
			}
		} else {
			plugin.DefaultRegistry.Remove(id)
		}
		return err
	}
	if entry := plugin.DefaultRegistry.Get(id); entry != nil && entry.Manifest != nil {
		p.Status.PluginVersion = entry.Manifest.Version
	}

	return nil
}

// DeletePlugin is the kopper delete callback. It stops the supervised
// process and drops the registry entry. The binary is left on disk —
// re-creating the CRD won't re-download a binary that already exists.
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
	if plugin.DefaultRegistry.Get(pluginID) == nil {
		entry, err := plugin.DefaultRegistry.Resolve(id)
		if err != nil {
			return err
		}
		if entry != nil {
			pluginID = entry.ID
		}
	}
	if err := machinery.StopPlugin(pluginID); err != nil {
		return err
	}
	plugin.DefaultRegistry.Remove(pluginID)
	return nil
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
