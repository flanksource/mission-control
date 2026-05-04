package supervisor

import (
	"fmt"
	"sync"

	dutyContext "github.com/flanksource/duty/context"
	goplugin "github.com/hashicorp/go-plugin"

	icplugin "github.com/flanksource/incident-commander/plugin"
	"github.com/flanksource/incident-commander/plugin/host"
	"github.com/flanksource/incident-commander/plugin/registry"
)

// active holds running supervisors so the kopper reconciler can stop them
// on CRD delete.
var (
	mu     sync.Mutex
	active = map[string]*Supervisor{}
)

// WireSupervisor installs the supervisor as the registry's start/stop hook.
// Must be called once at startup before the kopper reconciler is registered.
//
// The reconciler in plugin/registry stores plugin specs but does not import
// the supervisor package (that would create an import cycle); this function
// injects the start/stop callbacks at boot.
func WireSupervisor(ctx dutyContext.Context) {
	registry.SupervisorStarter = func(c dutyContext.Context, name string) error {
		return startPlugin(c, name)
	}
	registry.SupervisorStopper = func(name string) error {
		return stopPlugin(name)
	}
}

func startPlugin(ctx dutyContext.Context, name string) error {
	binPath := registry.BinaryPathFor(name)
	svc := host.New(ctx, name)

	// startHost is invoked after Dispense() so the broker is live. It opens
	// a listener on the broker, starts a gRPC server for this plugin's
	// HostService, and returns the broker id so the supervisor can pass it
	// to the plugin in RegisterPlugin.
	startHost := func(broker *goplugin.GRPCBroker) (uint32, error) {
		id := broker.NextId()
		go func() {
			lis, err := broker.Accept(id)
			if err != nil {
				ctx.Logger.Errorf("plugin %s: host broker accept: %v", name, err)
				return
			}
			s := icplugin.GRPCServerFactory(nil)
			svc.Register(s)
			if err := s.Serve(lis); err != nil {
				ctx.Logger.Debugf("plugin %s: host server stopped: %v", name, err)
			}
		}()
		return id, nil
	}

	sup := New(name, binPath)
	if err := sup.Start(ctx, startHost); err != nil {
		return fmt.Errorf("plugin %s: start supervisor: %w", name, err)
	}

	mu.Lock()
	active[name] = sup
	mu.Unlock()
	return nil
}

func stopPlugin(name string) error {
	mu.Lock()
	sup := active[name]
	delete(active, name)
	mu.Unlock()
	if sup != nil {
		sup.Stop()
	}
	return nil
}

// LookupSupervisor returns the running supervisor for a named plugin, or
// nil if the plugin is not running. Used by the echo handlers and CLI.
func LookupSupervisor(name string) *Supervisor {
	mu.Lock()
	defer mu.Unlock()
	return active[name]
}
