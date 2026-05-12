package supervisor

import (
	"fmt"
	"sync"

	dutyContext "github.com/flanksource/duty/context"
	"github.com/google/uuid"
	goplugin "github.com/hashicorp/go-plugin"

	"github.com/flanksource/incident-commander/plugin/adapter"
	"github.com/flanksource/incident-commander/plugin/host"
	"github.com/flanksource/incident-commander/plugin/registry"
)

// active holds running supervisors so the kopper reconciler can stop them
// on CRD delete.
var (
	mu     sync.Mutex
	active = map[uuid.UUID]*Supervisor{}
)

// Wire installs the supervisor as the registry's start/stop hook.
// Must be called once at startup before the kopper reconciler is registered.
//
// The reconciler in plugin/registry stores plugin specs but does not import
// the supervisor package (that would create an import cycle); this function
// injects the start/stop callbacks at boot.
func Wire(ctx dutyContext.Context) {
	registry.SupervisorStarter = func(c dutyContext.Context, id uuid.UUID) error {
		return startPlugin(c, id)
	}
	registry.SupervisorStopper = func(id uuid.UUID) error {
		return stopPlugin(id)
	}
}

func startPlugin(ctx dutyContext.Context, id uuid.UUID) error {
	entry := registry.Default.Get(id)
	if entry == nil {
		return fmt.Errorf("plugin %s: not registered", id)
	}
	binPath := registry.BinaryPathFor(entry.Name)
	svc := host.New(ctx, id)

	// startHost is invoked after Dispense() so the broker is live. It opens
	// a listener on the broker, starts a gRPC server for this plugin's
	// HostService, and returns the broker id so the supervisor can pass it
	// to the plugin in RegisterPlugin.
	startHost := func(broker *goplugin.GRPCBroker) (uint32, error) {
		brokerID := broker.NextId()
		go func() {
			lis, err := broker.Accept(brokerID)
			if err != nil {
				ctx.Logger.Errorf("plugin %s: host broker accept: %v", id, err)
				return
			}
			s := adapter.GRPCServerFactory(nil)
			svc.Register(s)
			if err := s.Serve(lis); err != nil {
				ctx.Logger.Debugf("plugin %s: host server stopped: %v", id, err)
			}
		}()
		return brokerID, nil
	}

	mu.Lock()
	if active[id] != nil {
		mu.Unlock()
		return nil
	}
	sup := New(id, binPath)
	active[id] = sup
	mu.Unlock()

	if err := sup.Start(ctx, startHost); err != nil {
		mu.Lock()
		if active[id] == sup {
			delete(active, id)
		}
		mu.Unlock()
		return fmt.Errorf("plugin %s: start supervisor: %w", id, err)
	}

	return nil
}

func stopPlugin(id uuid.UUID) error {
	mu.Lock()
	sup := active[id]
	delete(active, id)
	mu.Unlock()
	if sup != nil {
		sup.Stop()
	}
	return nil
}

// LookupSupervisor returns the running supervisor for a plugin id, or nil if
// the plugin is not running. Used by the echo handlers and CLI.
func LookupSupervisor(id uuid.UUID) *Supervisor {
	mu.Lock()
	defer mu.Unlock()
	return active[id]
}
