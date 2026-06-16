// ABOUTME: Re-resolves plugins pinned to "latest", swapping the running
// ABOUTME: process to a newly published binary and removing the old version.
package machinery

import (
	"errors"
	"fmt"

	dutyContext "github.com/flanksource/duty/context"
	"github.com/google/uuid"

	"github.com/flanksource/incident-commander/plugin"
	"github.com/flanksource/incident-commander/plugin/api"
	"github.com/flanksource/incident-commander/plugin/machinery/local"
)

// refreshOps are the side-effecting operations the refresh flow performs. They
// are injectable so tests can substitute the network install and the real
// process restart with fakes while still exercising the swap orchestration.
type refreshOps struct {
	resolveLatest func(ctx dutyContext.Context, name, source string) (string, string, error)
	stop          func(id uuid.UUID) error
	start         func(ctx dutyContext.Context, id uuid.UUID) error
	removeVersion func(name, source, version string) error
}

func defaultRefreshOps() refreshOps {
	return refreshOps{
		resolveLatest: local.ResolveAndInstallLatest,
		stop:          StopPlugin,
		start:         StartPlugin,
		removeVersion: local.RemoveVersion,
	}
}

// RefreshLatestPlugins re-resolves every plugin pinned to "latest" and restarts
// any whose resolved version has changed. Plugins pinned to a concrete version
// are left untouched. A failure on one plugin is logged and does not stop the
// others; all failures are joined into the returned error.
func RefreshLatestPlugins(ctx dutyContext.Context) error {
	return refreshLatestPlugins(ctx, defaultRefreshOps())
}

func refreshLatestPlugins(ctx dutyContext.Context, ops refreshOps) error {
	var errs []error
	for _, entry := range plugin.DefaultRegistry.List() {
		switch entry.Kind {
		case "", api.PluginKindLocal:
		default:
			continue
		}
		if !local.IsLatest(entry.Spec.Version) {
			continue
		}
		if err := ops.refreshPlugin(ctx, entry); err != nil {
			ctx.Logger.Errorf("plugin %s: refresh failed: %v", entry.Name, err)
			errs = append(errs, fmt.Errorf("plugin %s: %w", entry.Name, err))
		}
	}
	return errors.Join(errs...)
}

func (ops refreshOps) refreshPlugin(ctx dutyContext.Context, entry *plugin.Entry) error {
	_, newVersion, err := ops.resolveLatest(ctx, entry.Name, entry.Spec.Source)
	if err != nil {
		return err
	}

	oldVersion := local.VersionFromBinaryPath(entry.InstalledPath)
	if newVersion == oldVersion {
		ctx.Logger.V(3).Infof("plugin %s: already at latest version %s", entry.Name, newVersion)
		return nil
	}

	ctx.Logger.Infof("plugin %s: new version %s available (was %s); restarting", entry.Name, newVersion, oldVersion)
	if err := ops.stop(entry.ID); err != nil {
		return fmt.Errorf("stop plugin %s: %w", entry.Name, err)
	}
	if err := ops.start(ctx, entry.ID); err != nil {
		return fmt.Errorf("start plugin %s: %w", entry.Name, err)
	}

	if oldVersion != "" && oldVersion != newVersion {
		if err := ops.removeVersion(entry.Name, entry.Spec.Source, oldVersion); err != nil {
			ctx.Logger.Warnf("plugin %s: remove old version %s: %v", entry.Name, oldVersion, err)
		}
	}

	return nil
}
