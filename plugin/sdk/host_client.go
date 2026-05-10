package sdk

import (
	"context"
	"encoding/json"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/structpb"

	pluginpb "github.com/flanksource/incident-commander/plugin/proto"
)

// HostClient is the plugin-side handle to call back into the mission-control
// host. It is created by the SDK after the host opens the reverse-channel
// during RegisterPlugin and made available on every InvokeCtx.
//
// All methods enforce the Plugin CRD's connection allowlist on the host side —
// if a plugin requests a connection type it didn't declare, the host returns
// an error.
type HostClient interface {
	// GetConfigItem fetches a single config item by id. The host validates
	// that the calling user has read access before returning.
	GetConfigItem(ctx context.Context, id string) (*pluginpb.ConfigItem, error)

	// ListConfigs returns config items matching the given duty
	// ResourceSelector. selectorJSON is the JSON encoding of
	// types.ResourceSelector (the SDK passes it opaquely so plugin authors
	// don't need to import duty just to build a selector — they can
	// json.Marshal a map).
	ListConfigs(ctx context.Context, selectorJSON []byte) (*pluginpb.ConfigItemList, error)

	// GetConnection resolves a connection of the given type. typ is one of
	// "aws", "kubernetes", "gcp", "azure". If configItemID is set, the host
	// derives credentials from that config item using the same
	// SetupConnection() pipeline that powers playbook exec actions.
	GetConnection(ctx context.Context, typ, configItemID string) (*pluginpb.ResolvedConnection, error)

	// Log forwards a structured log entry to the host's logger. Plugins
	// should use this for cross-cutting events (audit trail, errors); regular
	// debug logging can go to stderr.
	Log(ctx context.Context, level, message string, fields map[string]string) error

	// WriteArtifact persists raw bytes via the host's artifact store and
	// returns a reference plugins can store and resolve later via
	// ReadArtifact.
	WriteArtifact(ctx context.Context, a *pluginpb.Artifact) (*pluginpb.ArtifactRef, error)

	// ReadArtifact retrieves an artifact previously written via the host.
	ReadArtifact(ctx context.Context, ref *pluginpb.ArtifactRef) (*pluginpb.Artifact, error)
}

// hostClient is the gRPC implementation of HostClient.
type hostClient struct {
	c pluginpb.HostServiceClient
}

func newHostClient(conn *grpc.ClientConn) HostClient {
	return &hostClient{c: pluginpb.NewHostServiceClient(conn)}
}

func (h *hostClient) GetConfigItem(ctx context.Context, id string) (*pluginpb.ConfigItem, error) {
	return h.c.GetConfigItem(ctx, &pluginpb.GetConfigItemRequest{Id: id})
}

func (h *hostClient) ListConfigs(ctx context.Context, selectorJSON []byte) (*pluginpb.ConfigItemList, error) {
	return h.c.ListConfigs(ctx, &pluginpb.ListConfigsRequest{SelectorJson: string(selectorJSON)})
}

func (h *hostClient) GetConnection(ctx context.Context, typ, configItemID string) (*pluginpb.ResolvedConnection, error) {
	return h.c.GetConnection(ctx, &pluginpb.GetConnectionRequest{Type: typ, ConfigItemId: configItemID})
}

func (h *hostClient) Log(ctx context.Context, level, message string, fields map[string]string) error {
	_, err := h.c.Log(ctx, &pluginpb.LogEntry{Level: level, Message: message, Fields: fields})
	return err
}

func (h *hostClient) WriteArtifact(ctx context.Context, a *pluginpb.Artifact) (*pluginpb.ArtifactRef, error) {
	return h.c.WriteArtifact(ctx, a)
}

func (h *hostClient) ReadArtifact(ctx context.Context, ref *pluginpb.ArtifactRef) (*pluginpb.Artifact, error) {
	return h.c.ReadArtifact(ctx, ref)
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
