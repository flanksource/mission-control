package machinery

import (
	"fmt"

	dutyAPI "github.com/flanksource/duty/api"
	dutyContext "github.com/flanksource/duty/context"
	goplugin "github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"

	"github.com/flanksource/incident-commander/plugin"
	"github.com/flanksource/incident-commander/plugin/gateway"
	"github.com/flanksource/incident-commander/plugin/machinery/local"
	"github.com/flanksource/incident-commander/plugin/registry"
	"github.com/google/uuid"
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
	svc := gateway.NewGRPCService(ctx, id)
	svc.SetPluginInvoker(func(invokeCtx dutyContext.Context, targetID uuid.UUID, req *plugin.InvokeRequest) (*plugin.InvokeResponse, error) {
		sup := local.LookupSupervisor(targetID)
		if sup == nil {
			return nil, invokeCtx.Oops().Code(dutyAPI.ENOTFOUND).Errorf("plugin %s not running", targetID)
		}
		return sup.Invoke(invokeCtx, req)
	})

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
			grpcServer := local.GRPCServerFactory([]grpc.ServerOption{
				grpc.UnaryInterceptor(svc.UnaryServerInterceptor()),
			})
			svc.Register(grpcServer)
			if err := grpcServer.Serve(lis); err != nil {
				ctx.Logger.Debugf("plugin %s: host server stopped: %v", id, err)
			}
		}()
		return brokerID, nil
	}

	sup := local.New(id, binPath)
	if !local.SetIfAbsent(id, sup) {
		return nil
	}

	if err := sup.Start(ctx, startHost); err != nil {
		local.RollbackSupervisor(id, sup)
		return fmt.Errorf("plugin %s: start supervisor: %w", id, err)
	}

	return nil
}

func stopPlugin(id uuid.UUID) error {
	sup := local.PopSupervisor(id)
	if sup != nil {
		sup.Stop()
	}
	return nil
}
