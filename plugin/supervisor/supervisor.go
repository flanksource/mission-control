// Package supervisor manages the lifecycle of plugin processes.
//
// Each Supervisor wraps a goplugin.Client that talks to one plugin binary.
// It launches the binary, completes the RegisterPlugin handshake, watches
// for unexpected exits, and restarts up to a rate-limited budget.
package supervisor

import (
	"context"
	gocontext "context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	goplugin "github.com/hashicorp/go-plugin"

	dutyContext "github.com/flanksource/duty/context"
	icplugin "github.com/flanksource/incident-commander/plugin"
	pluginpb "github.com/flanksource/incident-commander/plugin/proto"
	"github.com/flanksource/incident-commander/plugin/registry"
)

// Supervisor owns the lifecycle of one plugin process.
type Supervisor struct {
	Name       string
	BinaryPath string

	// Debounce window for binary-change events. Zero means use the default.
	Debounce time.Duration

	mu        sync.Mutex
	client    *goplugin.Client
	pluginCli *icplugin.Client
	manifest  *pluginpb.PluginManifest
	hostBrkID uint32
	startHost func(*goplugin.GRPCBroker) (uint32, error)
	stopped   bool

	restarts int
	window   time.Time

	// restartFn is invoked when the binary watcher decides to respawn the
	// plugin. Tests can substitute a counter; production uses (*Supervisor).restart.
	restartFn func(dutyContext.Context) error
}

const (
	maxRestartsPerHour = 10
	startupTimeout     = 30 * time.Second
	defaultDebounce    = 1 * time.Second
)

// New creates a Supervisor for a plugin binary.
func New(name, binaryPath string) *Supervisor {
	return &Supervisor{Name: name, BinaryPath: binaryPath}
}

// Start launches the plugin process and completes the RegisterPlugin
// handshake. It blocks until the manifest is received or startupTimeout
// expires.
//
// startHost is invoked after Dispense() succeeds (when the goplugin broker
// is available); it must register a HostService on the broker and return
// the broker id, which is then sent to the plugin in RegisterPlugin so the
// plugin can dial back through it.
func (s *Supervisor) Start(ctx dutyContext.Context, startHost func(broker *goplugin.GRPCBroker) (uint32, error)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client != nil {
		return errors.New("supervisor already started")
	}

	cmd := exec.Command(s.BinaryPath)
	cmd.Env = append(cmd.Env,
		fmt.Sprintf("%s=%s", icplugin.Handshake.MagicCookieKey, icplugin.Handshake.MagicCookieValue),
	)

	cli := goplugin.NewClient(&goplugin.ClientConfig{
		HandshakeConfig:  icplugin.Handshake,
		Plugins:          icplugin.PluginMap,
		Cmd:              cmd,
		AllowedProtocols: []goplugin.Protocol{goplugin.ProtocolGRPC},
		Managed:          true,
	})

	dialCtx, cancel := gocontext.WithTimeout(ctx, startupTimeout)
	defer cancel()

	rpcClient, err := cli.Client()
	if err != nil {
		cli.Kill()
		return fmt.Errorf("plugin %s rpc client: %w", s.Name, err)
	}

	raw, err := rpcClient.Dispense(icplugin.PluginName)
	if err != nil {
		cli.Kill()
		return fmt.Errorf("plugin %s dispense: %w", s.Name, err)
	}

	pluginCli, ok := raw.(*icplugin.Client)
	if !ok {
		cli.Kill()
		return fmt.Errorf("plugin %s: unexpected dispense type %T", s.Name, raw)
	}

	hostBrkID, err := startHost(pluginCli.Broker)
	if err != nil {
		cli.Kill()
		return fmt.Errorf("plugin %s: start host broker: %w", s.Name, err)
	}
	s.hostBrkID = hostBrkID

	manifest, err := pluginCli.Service.RegisterPlugin(dialCtx, &pluginpb.RegisterRequest{
		HostProtocolVersion: uint32(icplugin.ProtocolVersion),
		HostBrokerId:        hostBrkID,
	})
	if err != nil {
		cli.Kill()
		return fmt.Errorf("plugin %s RegisterPlugin: %w", s.Name, err)
	}

	s.client = cli
	s.pluginCli = pluginCli
	s.manifest = manifest
	s.startHost = startHost

	if err := registry.Default.SetManifest(s.Name, manifest); err != nil {
		// Not fatal — the registry might have been recreated, but the supervisor still works.
		ctx.Logger.Warnf("plugin %s: register manifest: %v", s.Name, err)
	}
	if err := registry.Default.SetSupervisor(s.Name, s); err != nil {
		ctx.Logger.Warnf("plugin %s: register supervisor: %v", s.Name, err)
	}

	go s.watchExit(ctx, cli)
	go s.watchBinary(ctx)

	ctx.Logger.Infof("plugin %s loaded: version=%q tabs=%d ops=%d ui_port=%d",
		s.Name, manifest.Version, len(manifest.Tabs), len(manifest.Operations), manifest.UiPort)
	return nil
}

// watchExit polls the goplugin client. If the plugin exits unexpectedly
// (i.e. not via Stop()), restart with rate limiting.
//
// cli is captured at goroutine start so that a binary-driven restart that
// replaces s.client doesn't leave this goroutine watching the new client
// forever. As soon as we observe that s.client is no longer cli, this
// goroutine exits cleanly and the new client's own watchExit takes over.
func (s *Supervisor) watchExit(ctx dutyContext.Context, cli *goplugin.Client) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		s.mu.Lock()
		current := s.client
		stopped := s.stopped
		s.mu.Unlock()
		if stopped || current != cli {
			return
		}
		if !cli.Exited() {
			continue
		}

		if !s.budgetOK() {
			ctx.Logger.Errorf("plugin %s exceeded restart budget; not restarting", s.Name)
			return
		}

		ctx.Logger.Warnf("plugin %s exited unexpectedly; restarting", s.Name)
		s.mu.Lock()
		s.client = nil
		s.pluginCli = nil
		startHost := s.startHost
		s.mu.Unlock()
		if err := s.Start(ctx, startHost); err != nil {
			ctx.Logger.Errorf("plugin %s restart failed: %v", s.Name, err)
		}
		return
	}
}

// watchBinary watches the directory containing the plugin binary and
// triggers a restart when the binary file is rewritten. Build pipelines
// emit several events for one logical update, so events are debounced.
func (s *Supervisor) watchBinary(ctx dutyContext.Context) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		ctx.Logger.Warnf("plugin %s: fsnotify new watcher: %v", s.Name, err)
		return
	}
	defer w.Close()

	dir := filepath.Dir(s.BinaryPath)
	if err := w.Add(dir); err != nil {
		ctx.Logger.Warnf("plugin %s: fsnotify watch %s: %v", s.Name, dir, err)
		return
	}

	debounce := s.Debounce
	if debounce <= 0 {
		debounce = defaultDebounce
	}

	var timer *time.Timer
	var fire <-chan time.Time

	for {
		select {
		case <-ctx.Done():
			return

		case ev, ok := <-w.Events:
			if !ok {
				return
			}
			s.mu.Lock()
			stopped := s.stopped
			s.mu.Unlock()
			if stopped {
				return
			}
			if !shouldTriggerRestart(ev, s.BinaryPath) {
				continue
			}
			if timer == nil {
				timer = time.NewTimer(debounce)
				fire = timer.C
			} else {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(debounce)
			}

		case err, ok := <-w.Errors:
			if !ok {
				return
			}
			ctx.Logger.Warnf("plugin %s: fsnotify error: %v", s.Name, err)

		case <-fire:
			timer = nil
			fire = nil
			ctx.Logger.Infof("plugin %s: binary changed, restarting", s.Name)
			restartFn := s.restartFn
			if restartFn == nil {
				restartFn = s.restart
			}
			if err := restartFn(ctx); err != nil {
				ctx.Logger.Errorf("plugin %s: restart failed: %v", s.Name, err)
			}
			return
		}
	}
}

// shouldTriggerRestart reports whether an fsnotify event for an entry in
// the watched directory should cause the plugin to be restarted. We react
// to writes, atomic renames-into-place, and creates (the new file landing
// after a temp+rename); plain chmod is ignored.
func shouldTriggerRestart(ev fsnotify.Event, target string) bool {
	if filepath.Clean(ev.Name) != filepath.Clean(target) {
		return false
	}
	return ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) != 0
}

// restart kills the running plugin process and starts a fresh one, subject
// to the existing per-hour restart budget.
func (s *Supervisor) restart(ctx dutyContext.Context) error {
	s.mu.Lock()
	oldCli := s.client
	startHost := s.startHost
	s.client = nil
	s.pluginCli = nil
	s.mu.Unlock()
	if oldCli != nil {
		oldCli.Kill()
	}
	if !s.budgetOK() {
		return fmt.Errorf("plugin %s: restart budget exceeded", s.Name)
	}
	return s.Start(ctx, startHost)
}

func (s *Supervisor) budgetOK() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if now.Sub(s.window) > time.Hour {
		s.window = now
		s.restarts = 0
	}
	s.restarts++
	return s.restarts <= maxRestartsPerHour
}

// Stop terminates the plugin process. Subsequent Start calls will create a
// fresh process.
func (s *Supervisor) Stop() {
	s.mu.Lock()
	s.stopped = true
	cli := s.client
	s.mu.Unlock()
	if cli != nil {
		cli.Kill()
	}
}

// Manifest returns the most recent PluginManifest received from the plugin.
func (s *Supervisor) Manifest() *pluginpb.PluginManifest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.manifest
}

// UIPort is a convenience for the most common manifest field.
func (s *Supervisor) UIPort() uint32 {
	m := s.Manifest()
	if m == nil {
		return 0
	}
	return m.UiPort
}

// Invoke calls the plugin's Invoke RPC.
func (s *Supervisor) Invoke(ctx context.Context, req *pluginpb.InvokeRequest) (*pluginpb.InvokeResponse, error) {
	s.mu.Lock()
	pluginCli := s.pluginCli
	s.mu.Unlock()
	if pluginCli == nil {
		return nil, fmt.Errorf("plugin %s not running", s.Name)
	}
	return pluginCli.Service.Invoke(ctx, req)
}

// ListOperations calls the plugin's ListOperations RPC.
func (s *Supervisor) ListOperations(ctx context.Context) (*pluginpb.OperationList, error) {
	s.mu.Lock()
	pluginCli := s.pluginCli
	s.mu.Unlock()
	if pluginCli == nil {
		return nil, fmt.Errorf("plugin %s not running", s.Name)
	}
	return pluginCli.Service.ListOperations(ctx, &pluginpb.Empty{})
}
