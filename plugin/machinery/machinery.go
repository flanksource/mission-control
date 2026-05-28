package machinery

import (
	"fmt"
	"net/url"

	dutyAPI "github.com/flanksource/duty/api"
	dutyContext "github.com/flanksource/duty/context"
	"github.com/google/uuid"
	goplugin "github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"

	"github.com/flanksource/incident-commander/plugin"
	"github.com/flanksource/incident-commander/plugin/machinery/local"
)

func StartPlugin(ctx dutyContext.Context, id uuid.UUID) error {
	entry := plugin.DefaultRegistry.Get(id)
	if entry == nil {
		return fmt.Errorf("plugin %s: not registered", id)
	}

	switch entry.Kind {
	case plugin.PluginKindLocal:
		return startLocalPlugin(ctx, entry)
	case plugin.PluginKindProxied:
		return nil
	default:
		return fmt.Errorf("plugin %s: unsupported connection kind %q", id, entry.Kind)
	}
}

func startLocalPlugin(ctx dutyContext.Context, entry *plugin.Entry) error {
	installedPath, err := local.InstallPlugin(ctx, entry.Name, entry.Spec.Source, entry.Spec.Version)
	if err != nil {
		return fmt.Errorf("failed to install plugin %s: %w", entry.Name, err)
	}
	if err := plugin.DefaultRegistry.SetInstalledPath(entry.ID, installedPath); err != nil {
		return err
	}
	entry.InstalledPath = installedPath

	svc := NewGRPCService(ctx)

	// startHost is invoked after Dispense() so the broker is live. It opens
	// a listener on the broker, starts a gRPC server for this plugin's
	// HostService, and returns the broker id so the supervisor can pass it
	// to the plugin in RegisterPlugin.
	startHost := func(broker *goplugin.GRPCBroker) (uint32, error) {
		brokerID := broker.NextId()
		go func() {
			lis, err := broker.Accept(brokerID)
			if err != nil {
				ctx.Logger.Errorf("plugin %s: host broker accept: %v", entry.ID, err)
				return
			}

			upstreamConn, err := newUpstreamHostConn(ctx)
			if err != nil {
				ctx.Logger.Warnf("plugin %s: upstream host grpc unavailable: %v", entry.ID, err)
			}
			if upstreamConn != nil {
				defer upstreamConn.Close()
			}

			var opts []grpc.ServerOption
			if upstreamConn != nil {
				opts = append(opts, grpc.UnknownServiceHandler(agentHostUnknownServiceHandler(svc, upstreamConn)))
			} else {
				opts = append(opts, grpc.UnaryInterceptor(svc.UnaryServerInterceptor()))
			}

			grpcServer := local.GRPCServerFactory(opts)
			if upstreamConn == nil {
				svc.Register(grpcServer)
			}
			if err := grpcServer.Serve(lis); err != nil {
				ctx.Logger.Debugf("plugin %s: host server stopped: %v", entry.ID, err)
			}
		}()
		return brokerID, nil
	}

	return startLocalPluginWithHost(ctx, entry, startHost)
}

func startLocalPluginWithHost(ctx dutyContext.Context, entry *plugin.Entry, startHost func(*goplugin.GRPCBroker) (uint32, error)) error {
	sup := local.New(entry.ID, entry.InstalledPath)
	started, err := plugin.DefaultRegistry.SetRuntimeIfAbsent(entry.ID, sup)
	if err != nil {
		return err
	}
	if !started {
		return nil
	}

	if err := sup.Start(ctx, startHost); err != nil {
		plugin.DefaultRegistry.PopRuntime(entry.ID)
		return fmt.Errorf("plugin %s: start supervisor: %w", entry.ID, err)
	}

	return nil
}

func StopPlugin(id uuid.UUID) error {
	runtime := plugin.DefaultRegistry.PopRuntime(id)
	if runtime != nil {
		runtime.Stop()
	}
	return nil
}

func Invoke(ctx dutyContext.Context, pluginID uuid.UUID, req *plugin.InvokeRequest) (*plugin.InvokeResponse, error) {
	entry := plugin.DefaultRegistry.Get(pluginID)
	if entry == nil {
		return nil, ctx.Oops().Code(dutyAPI.ENOTFOUND).Errorf("plugin %s not registered", pluginID)
	}

	switch entry.Kind {
	case plugin.PluginKindLocal:
		if entry.Runtime == nil {
			return nil, ctx.Oops().Code(dutyAPI.ENOTFOUND).Errorf("plugin %s not running", pluginID)
		}
		return entry.Runtime.Invoke(ctx, req)
	default:
		return nil, ctx.Oops().Code(dutyAPI.EINVALID).Errorf("plugin %s has unsupported connection kind %q", pluginID, entry.Kind)
	}
}

func HTTPURL(ctx dutyContext.Context, pluginID uuid.UUID) (*url.URL, error) {
	entry := plugin.DefaultRegistry.Get(pluginID)
	if entry == nil {
		return nil, ctx.Oops().Code(dutyAPI.ENOTFOUND).Errorf("plugin %s not registered", pluginID)
	}

	switch entry.Kind {
	case plugin.PluginKindLocal:
		if entry.Runtime == nil {
			return nil, ctx.Oops().Code(dutyAPI.ENOTFOUND).Errorf("plugin %s not running", pluginID)
		}
		port := entry.Runtime.UIPort()
		if port == 0 {
			return nil, ctx.Oops().Code(dutyAPI.EINTERNAL).Errorf("plugin %s did not advertise a UI port", pluginID)
		}
		return url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
	default:
		return nil, ctx.Oops().Code(dutyAPI.EINVALID).Errorf("plugin %s has unsupported connection kind %q", pluginID, entry.Kind)
	}
}
