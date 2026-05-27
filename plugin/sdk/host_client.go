package sdk

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/flanksource/duty/types"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/structpb"

	pluginpb "github.com/flanksource/incident-commander/plugin"
)

// HostClient is the plugin-side handle to call back into the mission-control
// host. It is created by the SDK after the host opens the reverse-channel
// during RegisterPlugin and made available on every InvokeCtx.
type HostClient interface {
	// GetConfigItem fetches a single config item by id.
	GetConfigItem(ctx context.Context, id string) (*pluginpb.ConfigItem, error)

	// ListConfigs returns config items matching the given ResourceSelector.
	ListConfigs(ctx context.Context, selector types.ResourceSelector, limit int) (*pluginpb.ConfigItemList, error)

	// GetConnectionByType resolves a connection using spec.connections.types.
	GetConnectionByType(ctx context.Context, typ ConnectionType) (*pluginpb.ResolvedConnection, error)

	// GetConnectionForConfig resolves the connection used by the scraper that created the config item.
	GetConnectionForConfig(ctx context.Context, configItemID string) (*pluginpb.ResolvedConnection, error)

	// GetConnectionByID resolves a Mission Control connection by id.
	GetConnectionByID(ctx context.Context, connectionID string) (*pluginpb.ResolvedConnection, error)

	// GetConnectionByLabel resolves a connection using spec.connections.labels.
	GetConnectionByLabel(ctx context.Context, label string) (*pluginpb.ResolvedConnection, error)

	// Log forwards a structured log entry to the host's logger.
	Log(ctx context.Context, level, message string, fields map[string]string) error

	// InvokePlugin invokes another plugin operation through Mission Control.
	InvokePlugin(ctx context.Context, plugin, operation, configItemID string, params json.RawMessage) (*pluginpb.InvokeResponse, error)

	// WriteArtifact persists raw bytes via the host's artifact store and returns a reference.
	WriteArtifact(ctx context.Context, a *pluginpb.Artifact) (*pluginpb.ArtifactRef, error)

	// ReadArtifact retrieves an artifact previously written via the host.
	ReadArtifact(ctx context.Context, ref *pluginpb.ArtifactRef) (*pluginpb.Artifact, error)
}

type hostClient struct {
	c               pluginpb.HostServiceClient
	invocationToken string
}

func newHostClient(conn *grpc.ClientConn, token string) *hostClient {
	return &hostClient{c: pluginpb.NewHostServiceClient(conn), invocationToken: token}
}

func (h *hostClient) authContext(ctx context.Context) context.Context {
	return withInvocationToken(ctx, h.invocationToken)
}

func (h *hostClient) GetConfigItem(ctx context.Context, id string) (*pluginpb.ConfigItem, error) {
	return h.c.GetConfigItem(h.authContext(ctx), &pluginpb.GetConfigItemRequest{Id: id})
}

func (h *hostClient) ListConfigs(ctx context.Context, selector types.ResourceSelector, limit int) (*pluginpb.ConfigItemList, error) {
	return h.c.ListConfigs(h.authContext(ctx), &pluginpb.ListConfigsRequest{
		Selector: pluginpb.ResourceSelectorFromDuty(selector),
		Limit:    int32(limit),
	})
}

func (h *hostClient) GetConnectionByType(ctx context.Context, typ ConnectionType) (*pluginpb.ResolvedConnection, error) {
	return h.c.GetConnection(h.authContext(ctx), &pluginpb.GetConnectionRequest{Lookup: &pluginpb.GetConnectionRequest_Type{Type: string(typ)}})
}

func (h *hostClient) GetConnectionForConfig(ctx context.Context, configItemID string) (*pluginpb.ResolvedConnection, error) {
	return h.c.GetConnection(h.authContext(ctx), &pluginpb.GetConnectionRequest{Lookup: &pluginpb.GetConnectionRequest_ConfigItemId{ConfigItemId: configItemID}})
}

func (h *hostClient) GetConnectionByID(ctx context.Context, connectionID string) (*pluginpb.ResolvedConnection, error) {
	return h.c.GetConnection(h.authContext(ctx), &pluginpb.GetConnectionRequest{Lookup: &pluginpb.GetConnectionRequest_ConnectionId{ConnectionId: connectionID}})
}

func (h *hostClient) GetConnectionByLabel(ctx context.Context, label string) (*pluginpb.ResolvedConnection, error) {
	return h.c.GetConnection(h.authContext(ctx), &pluginpb.GetConnectionRequest{Lookup: &pluginpb.GetConnectionRequest_Label{Label: label}})
}

func (h *hostClient) Log(ctx context.Context, level, message string, fields map[string]string) error {
	_, err := h.c.Log(h.authContext(ctx), &pluginpb.LogEntry{Level: level, Message: message, Fields: fields})
	return err
}

func (h *hostClient) InvokePlugin(ctx context.Context, plugin, operation, configItemID string, params json.RawMessage) (*pluginpb.InvokeResponse, error) {
	return h.c.InvokePlugin(h.authContext(ctx), &pluginpb.InvokePluginRequest{
		Plugin:       plugin,
		Operation:    operation,
		ConfigItemId: configItemID,
		ParamsJson:   params,
	})
}

func (h *hostClient) WriteArtifact(ctx context.Context, a *pluginpb.Artifact) (*pluginpb.ArtifactRef, error) {
	return h.c.WriteArtifact(h.authContext(ctx), a)
}

func (h *hostClient) ReadArtifact(ctx context.Context, ref *pluginpb.ArtifactRef) (*pluginpb.Artifact, error) {
	return h.c.ReadArtifact(h.authContext(ctx), ref)
}

// settingsFromStruct decodes a *structpb.Struct into a JSON-shaped map[string]any.
// Used when passing CRD spec.properties through Configure().
func settingsFromStruct(s *structpb.Struct) (map[string]any, error) {
	if s == nil {
		return map[string]any{}, nil
	}
	b, err := s.MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("encode settings: %w", err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("decode settings: %w", err)
	}
	return out, nil
}
