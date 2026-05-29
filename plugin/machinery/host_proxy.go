package machinery

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/flanksource/incident-commander/api"
	pluginpb "github.com/flanksource/incident-commander/plugin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/dynamicpb"
)

func agentHostUnknownServiceHandler(svc *Service, upstreamConn *grpc.ClientConn) grpc.StreamHandler {
	return func(_ any, stream grpc.ServerStream) error {
		method, ok := grpc.MethodFromServerStream(stream)
		if !ok || method == "" {
			return status.Error(codes.Internal, "grpc method is required")
		}

		if method == pluginpb.HostService_GetConnection_FullMethodName {
			return handleLocalGetConnection(svc, stream)
		}
		if upstreamConn == nil {
			return status.Error(codes.Unavailable, "upstream host grpc is not configured")
		}
		return proxyHostUnaryCall(svc, stream, upstreamConn, method)
	}
}

func handleLocalGetConnection(svc *Service, stream grpc.ServerStream) error {
	ctx, err := svc.contextWithInvocation(stream.Context())
	if err != nil {
		return err
	}

	req := new(pluginpb.GetConnectionRequest)
	if err := stream.RecvMsg(req); err != nil {
		return err
	}
	resp, err := svc.GetConnection(ctx, req)
	if err != nil {
		return err
	}
	return stream.SendMsg(resp)
}

func proxyHostUnaryCall(svc *Service, stream grpc.ServerStream, conn *grpc.ClientConn, method string) error {
	invocationCtx, err := svc.contextWithInvocation(stream.Context())
	if err != nil {
		return err
	}
	inputType, outputType, err := hostServiceMethodTypes(method)
	if err != nil {
		return err
	}

	in := dynamicpb.NewMessage(inputType)
	if err := stream.RecvMsg(in); err != nil {
		return err
	}

	out := dynamicpb.NewMessage(outputType)
	callCtx := invocationCtx
	if md, ok := metadata.FromIncomingContext(stream.Context()); ok {
		callCtx = metadata.NewOutgoingContext(callCtx, md.Copy())
	}
	if api.UpstreamConf.AgentName != "" {
		callCtx = metadata.AppendToOutgoingContext(callCtx, "agent_name", api.UpstreamConf.AgentName)
	}
	if err := conn.Invoke(callCtx, method, in, out); err != nil {
		return err
	}
	return stream.SendMsg(out)
}

func hostServiceMethodTypes(fullMethod string) (protoreflect.MessageDescriptor, protoreflect.MessageDescriptor, error) {
	serviceName, methodName, ok := strings.Cut(strings.TrimPrefix(fullMethod, "/"), "/")
	if !ok {
		return nil, nil, status.Errorf(codes.Unimplemented, "invalid grpc method %q", fullMethod)
	}

	desc, err := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName(serviceName))
	if err != nil {
		return nil, nil, status.Errorf(codes.Unimplemented, "service %s: %v", serviceName, err)
	}
	service, ok := desc.(protoreflect.ServiceDescriptor)
	if !ok {
		return nil, nil, status.Errorf(codes.Unimplemented, "%s is not a service", serviceName)
	}
	method := service.Methods().ByName(protoreflect.Name(methodName))
	if method == nil {
		return nil, nil, status.Errorf(codes.Unimplemented, "method %s not found", fullMethod)
	}
	return method.Input(), method.Output(), nil
}

func upstreamGRPCTarget(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse upstream host: %w", err)
	}
	host := u.Hostname()
	if host == "" {
		return "", fmt.Errorf("upstream host is required")
	}
	return net.JoinHostPort(host, fmt.Sprintf("%d", api.UpstreamGRPCPort)), nil
}

type upstreamBasicAuthCredentials struct{}

func (upstreamBasicAuthCredentials) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	if api.UpstreamConf.Username == "" && api.UpstreamConf.Password == "" {
		return nil, nil
	}
	token := base64.StdEncoding.EncodeToString([]byte(api.UpstreamConf.Username + ":" + api.UpstreamConf.Password))
	return map[string]string{"authorization": "Basic " + token}, nil
}

func (upstreamBasicAuthCredentials) RequireTransportSecurity() bool {
	return false
}

func newUpstreamHostConn(ctx context.Context) (*grpc.ClientConn, error) {
	if !api.UpstreamConf.Valid() {
		return nil, nil
	}
	secure := strings.HasPrefix(api.UpstreamConf.Host, "https://")
	target, err := upstreamGRPCTarget(api.UpstreamConf.Host)
	if err != nil {
		return nil, err
	}
	transportCredentials := credentials.TransportCredentials(insecure.NewCredentials())
	if secure {
		transportCredentials = credentials.NewTLS(&tls.Config{InsecureSkipVerify: api.UpstreamConf.InsecureSkipVerify})
	}
	return grpc.DialContext(ctx, target,
		grpc.WithTransportCredentials(transportCredentials),
		grpc.WithPerRPCCredentials(upstreamBasicAuthCredentials{}),
	)
}
