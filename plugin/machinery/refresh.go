// ABOUTME: Re-resolves plugins pinned to "latest", swapping the running
// ABOUTME: process to a newly published binary and removing the old version.
package machinery

import (
	"errors"
	"fmt"

	dutyAPI "github.com/flanksource/duty/api"
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

// LatestPluginRefreshResult describes the result of resolving and optionally
// restarting a plugin that tracks latest.
type LatestPluginRefreshResult struct {
	PluginID        uuid.UUID `json:"-"`
	Plugin          string    `json:"plugin"`
	PreviousVersion string    `json:"previousVersion,omitempty"`
	ResolvedVersion string    `json:"resolvedVersion,omitempty"`
	InstalledPath   string    `json:"installedPath,omitempty"`
	Restarted       bool      `json:"restarted"`
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

func RefreshLatestPlugin(ctx dutyContext.Context, entry *plugin.Entry) (*LatestPluginRefreshResult, error) {
	if entry == nil {
		return nil, dutyAPI.Errorf(dutyAPI.EINVALID, "plugin is required")
	}
	if entry.Kind != "" && entry.Kind != api.PluginKindLocal {
		return nil, dutyAPI.Errorf(dutyAPI.EINVALID, "plugin %s has unsupported connection kind %q", entry.Name, entry.Kind)
	}
	if !local.IsLatest(entry.Spec.Version) {
		return nil, dutyAPI.Errorf(dutyAPI.EINVALID, "plugin %s is pinned to version %q", entry.Name, entry.Spec.Version)
	}
	return defaultRefreshOps().refreshPlugin(ctx, entry)
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
		if _, err := ops.refreshPlugin(ctx, entry); err != nil {
			ctx.Logger.Errorf("plugin %s: refresh failed: %v", entry.Name, err)
			errs = append(errs, fmt.Errorf("plugin %s: %w", entry.Name, err))
		}
	}
	return errors.Join(errs...)
}

func (ops refreshOps) refreshPlugin(ctx dutyContext.Context, entry *plugin.Entry) (*LatestPluginRefreshResult, error) {
	installedPath, newVersion, err := ops.resolveLatest(ctx, entry.Name, entry.Spec.Source)
	if err != nil {
		return nil, err
	}

	oldVersion := local.VersionFromBinaryPath(entry.InstalledPath)
	result := &LatestPluginRefreshResult{
		PluginID:        entry.ID,
		Plugin:          entry.Name,
		PreviousVersion: oldVersion,
		ResolvedVersion: newVersion,
		InstalledPath:   installedPath,
	}
	if newVersion == oldVersion {
		ctx.Logger.V(3).Infof("plugin %s: already at latest version %s", entry.Name, newVersion)
		return result, nil
	}

	ctx.Logger.Infof("plugin %s: new version %s available (was %s); restarting", entry.Name, newVersion, oldVersion)
	if err := ops.stop(entry.ID); err != nil {
		return nil, fmt.Errorf("stop plugin %s: %w", entry.Name, err)
	}
	if err := ops.start(ctx, entry.ID); err != nil {
		return nil, fmt.Errorf("start plugin %s: %w", entry.Name, err)
	}
	result.Restarted = true

	if updated := plugin.DefaultRegistry.Get(entry.ID); updated != nil && updated.InstalledPath != "" {
		result.InstalledPath = updated.InstalledPath
		result.ResolvedVersion = local.VersionFromBinaryPath(updated.InstalledPath)
	}

	if oldVersion != "" && oldVersion != newVersion {
		if err := ops.removeVersion(entry.Name, entry.Spec.Source, oldVersion); err != nil {
			ctx.Logger.Warnf("plugin %s: remove old version %s: %v", entry.Name, oldVersion, err)
		}
	}

	return result, nil
}
