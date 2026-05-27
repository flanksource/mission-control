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
	"github.com/google/uuid"
)

func StartPlugin(ctx dutyContext.Context, id uuid.UUID) error {
	entry := plugin.DefaultRegistry.Get(id)
	if entry == nil {
		return fmt.Errorf("plugin %s: not registered", id)
	}

	binPath := local.BinaryPathFor(entry.Name)
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

func StopPlugin(id uuid.UUID) error {
	sup := local.PopSupervisor(id)
	if sup != nil {
		sup.Stop()
	}
	return nil
}
