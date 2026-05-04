// Package host implements the HostService gRPC server — the back-channel
// that runs in the mission-control process and is dialed by every plugin
// during RegisterPlugin.
//
// All RPCs operate in the calling plugin's identity (matched via the
// peer-info that go-plugin's broker adds to the gRPC context).
package host

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	dutyConn "github.com/flanksource/duty/connection"
	dutyContext "github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	lru "github.com/hashicorp/golang-lru/v2/expirable"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "github.com/flanksource/incident-commander/api/v1"
	pluginpb "github.com/flanksource/incident-commander/plugin/proto"
	"github.com/flanksource/incident-commander/plugin/registry"
)

// connectionCacheTTL is how long a resolved connection stays cached on the
// host. Plugins re-fetch on cache miss, which is rare in practice (most
// operations make many calls within the TTL).
const connectionCacheTTL = 5 * time.Minute

type connKey struct {
	plugin   string
	typ      string
	configID string
}

// Service is the host-side gRPC server. There is one per plugin process —
// the supervisor instantiates it during Start() so it can stamp the plugin
// name into requests for allowlist enforcement and caching.
type Service struct {
	pluginpb.UnimplementedHostServiceServer

	pluginName string
	ctx        dutyContext.Context

	// connCache memoises GetConnection results across calls within a single
	// plugin process. Keyed by (plugin, type, configID).
	connCache *lru.LRU[connKey, *pluginpb.ResolvedConnection]
}

// New creates a host Service for one named plugin. Multiple plugins running
// concurrently get separate Services so the connection allowlist (read off
// the Plugin CRD via the registry) is enforced per-plugin.
func New(ctx dutyContext.Context, pluginName string) *Service {
	cache := lru.NewLRU[connKey, *pluginpb.ResolvedConnection](256, nil, connectionCacheTTL)
	return &Service{
		pluginName: pluginName,
		ctx:        ctx,
		connCache:  cache,
	}
}

// Register exposes the service on the given gRPC server.
func (s *Service) Register(g *grpc.Server) {
	pluginpb.RegisterHostServiceServer(g, s)
}

func (s *Service) GetConfigItem(ctx context.Context, req *pluginpb.GetConfigItemRequest) (*pluginpb.ConfigItem, error) {
	if req.Id == "" {
		return nil, fmt.Errorf("id is required")
	}
	var item models.ConfigItem
	if err := s.ctx.DB().WithContext(ctx).Where("id = ?", req.Id).First(&item).Error; err != nil {
		return nil, fmt.Errorf("config item %s: %w", req.Id, err)
	}
	return configItemToProto(&item)
}

func (s *Service) ListConfigs(ctx context.Context, req *pluginpb.ListConfigsRequest) (*pluginpb.ConfigItemList, error) {
	var sel types.ResourceSelector
	if req.SelectorJson != "" {
		if err := json.Unmarshal([]byte(req.SelectorJson), &sel); err != nil {
			return nil, fmt.Errorf("decode selector: %w", err)
		}
	}
	q := s.ctx.DB().WithContext(ctx).Model(&models.ConfigItem{})
	if len(sel.Types) > 0 {
		q = q.Where("type IN ?", sel.Types)
	}
	if sel.Namespace != "" {
		q = q.Where("? = ANY(tags->>'namespace')", sel.Namespace)
	}
	if req.Limit > 0 {
		q = q.Limit(int(req.Limit))
	}
	var items []models.ConfigItem
	if err := q.Find(&items).Error; err != nil {
		return nil, err
	}
	out := &pluginpb.ConfigItemList{}
	for i := range items {
		ci, err := configItemToProto(&items[i])
		if err != nil {
			return nil, err
		}
		out.Items = append(out.Items, ci)
	}
	return out, nil
}

func (s *Service) GetConnection(ctx context.Context, req *pluginpb.GetConnectionRequest) (*pluginpb.ResolvedConnection, error) {
	if req.Type == "" {
		return nil, fmt.Errorf("type is required")
	}

	key := connKey{plugin: s.pluginName, typ: req.Type, configID: req.ConfigItemId}
	if cached, ok := s.connCache.Get(key); ok {
		return cached, nil
	}

	entry := registry.Default.Get(s.pluginName)
	if entry == nil {
		return nil, fmt.Errorf("plugin %q is not registered", s.pluginName)
	}

	resolved, err := resolveConnection(s.ctx, entry.Spec, req.Type, req.ConfigItemId)
	if err != nil {
		return nil, err
	}

	s.connCache.Add(key, resolved)
	return resolved, nil
}

func (s *Service) Log(ctx context.Context, e *pluginpb.LogEntry) (*pluginpb.Empty, error) {
	logger := s.ctx.Logger
	args := make([]any, 0, len(e.Fields)*2)
	for k, v := range e.Fields {
		args = append(args, k, v)
	}
	prefix := fmt.Sprintf("[plugin %s] %s", s.pluginName, e.Message)
	switch e.Level {
	case "debug":
		logger.Debugf(prefix, args...)
	case "warn":
		logger.Warnf(prefix, args...)
	case "error":
		logger.Errorf(prefix, args...)
	default:
		logger.Infof(prefix, args...)
	}
	return &pluginpb.Empty{}, nil
}

// WriteArtifact / ReadArtifact are stubbed for the MVP.
// The artifact store integration is straight-forward (artifacts.Default in
// this codebase) but is not exercised by Phase 0–4 of the plugin plan.
func (s *Service) WriteArtifact(ctx context.Context, a *pluginpb.Artifact) (*pluginpb.ArtifactRef, error) {
	return nil, fmt.Errorf("WriteArtifact: not implemented")
}

func (s *Service) ReadArtifact(ctx context.Context, ref *pluginpb.ArtifactRef) (*pluginpb.Artifact, error) {
	return nil, fmt.Errorf("ReadArtifact: not implemented")
}

// readDefaultKubeconfig returns the kubeconfig contents the duty kubernetes
// fallback would have used: $KUBECONFIG (first existing entry), then
// $HOME/.kube/config. Plugins run as separate processes, so the host has to
// ship the bytes — the in-cluster auth path is intentionally not handled here
// (a plugin that needs in-cluster creds should declare its own connection).
func readDefaultKubeconfig() (string, error) {
	var candidates []string
	if v := os.Getenv("KUBECONFIG"); v != "" {
		candidates = append(candidates, filepath.SplitList(v)...)
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".kube", "config"))
	}
	for _, p := range candidates {
		b, err := os.ReadFile(p)
		if err == nil {
			return string(b), nil
		}
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("read %s: %w", p, err)
		}
	}
	return "", fmt.Errorf("no kubeconfig declared on plugin and no default kubeconfig found")
}

// resolveConnection looks up the matching field in the Plugin CRD's spec
// and returns the resolved credentials. It mirrors the allowlist behavior
// of playbook actions[].exec.connections: a plugin asking for a type the
// CRD didn't declare gets an error.
func resolveConnection(ctx dutyContext.Context, spec v1.PluginSpec, typ, configItemID string) (*pluginpb.ResolvedConnection, error) {
	ec := spec.Connections
	switch typ {
	case "kubernetes":
		if ec.Kubernetes == nil {
			return nil, fmt.Errorf("plugin did not declare a kubernetes connection")
		}
		if _, _, err := ec.Kubernetes.Populate(ctx, true); err != nil {
			return nil, fmt.Errorf("hydrate kubernetes: %w", err)
		}
		kubeconfig := ""
		if ec.Kubernetes.Kubeconfig != nil {
			kubeconfig = ec.Kubernetes.Kubeconfig.ValueStatic
		}
		if kubeconfig == "" {
			fallback, err := readDefaultKubeconfig()
			if err != nil {
				return nil, fmt.Errorf("resolve kubeconfig: %w", err)
			}
			kubeconfig = fallback
		}
		props, _ := structpb.NewStruct(map[string]any{
			"kubeconfig": kubeconfig,
		})
		return &pluginpb.ResolvedConnection{
			Type:       "kubernetes",
			Properties: props,
		}, nil
	case "aws":
		if ec.AWS == nil {
			return nil, fmt.Errorf("plugin did not declare an aws connection")
		}
		if err := ec.AWS.Populate(ctx); err != nil {
			return nil, fmt.Errorf("hydrate aws: %w", err)
		}
		props, _ := structpb.NewStruct(map[string]any{
			"region":   ec.AWS.Region,
			"endpoint": ec.AWS.Endpoint,
		})
		return &pluginpb.ResolvedConnection{
			Type:       "aws",
			Username:   ec.AWS.AccessKey.ValueStatic,
			Password:   ec.AWS.SecretKey.ValueStatic,
			Token:      ec.AWS.SessionToken.ValueStatic,
			Properties: props,
			ExpiresAt:  timestamppb.New(time.Now().Add(connectionCacheTTL)),
		}, nil
	case "gcp":
		if ec.GCP == nil {
			return nil, fmt.Errorf("plugin did not declare a gcp connection")
		}
		if err := ec.GCP.HydrateConnection(ctx); err != nil {
			return nil, fmt.Errorf("hydrate gcp: %w", err)
		}
		var creds string
		if ec.GCP.Credentials != nil {
			creds = ec.GCP.Credentials.ValueStatic
		}
		return &pluginpb.ResolvedConnection{
			Type:        "gcp",
			Url:         ec.GCP.Endpoint,
			Certificate: creds,
		}, nil
	case "sql":
		return resolveSQLConnection(ctx, spec, configItemID)
	case "azure":
		if ec.Azure == nil {
			return nil, fmt.Errorf("plugin did not declare an azure connection")
		}
		if err := ec.Azure.HydrateConnection(ctx); err != nil {
			return nil, fmt.Errorf("hydrate azure: %w", err)
		}
		var clientID, clientSecret string
		if ec.Azure.ClientID != nil {
			clientID = ec.Azure.ClientID.ValueStatic
		}
		if ec.Azure.ClientSecret != nil {
			clientSecret = ec.Azure.ClientSecret.ValueStatic
		}
		props, _ := structpb.NewStruct(map[string]any{
			"tenantID": ec.Azure.TenantID,
		})
		return &pluginpb.ResolvedConnection{
			Type:       "azure",
			Username:   clientID,
			Password:   clientSecret,
			Properties: props,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported connection type %q", typ)
	}
}

func configItemToProto(item *models.ConfigItem) (*pluginpb.ConfigItem, error) {
	out := &pluginpb.ConfigItem{
		Id: item.ID.String(),
	}
	if item.Name != nil {
		out.Name = *item.Name
	}
	if item.Type != nil {
		out.Type = *item.Type
	}
	if item.AgentID != uuid.Nil {
		out.AgentId = item.AgentID.String()
	}
	if item.Health != nil {
		out.Health = string(*item.Health)
	}
	if item.Status != nil {
		out.Status = *item.Status
	}
	if item.Tags != nil {
		out.Tags = map[string]string(item.Tags)
	}
	if item.Labels != nil {
		out.Labels = map[string]string(*item.Labels)
	}
	if item.Properties != nil {
		props := map[string]any{}
		for _, p := range *item.Properties {
			props[p.Name] = p.Text
		}
		s, err := structpb.NewStruct(props)
		if err == nil {
			out.Properties = s
		}
	}
	if item.Config != nil && *item.Config != "" {
		var cfg map[string]any
		if err := json.Unmarshal([]byte(*item.Config), &cfg); err == nil {
			s, _ := structpb.NewStruct(cfg)
			out.Config = s
		}
	}
	return out, nil
}

// resolveSQLConnection resolves a SQL (sql_server / postgres / mysql)
// connection for the plugin. Resolution order:
//
//  1. Plugin spec: if PluginSpec.SQLConnection is set, hydrate and return it.
//     This pins the plugin to a single database regardless of which catalog
//     item the iframe is on.
//  2. Scraper inheritance: treat configItemID as a ConfigItem id, walk to
//     its owning ScrapeConfig, and pull the first non-empty
//     `spec.sql[].connection` reference. The user already declared the
//     connection on the scraper that ingested this config item, so we
//     reuse it.
func resolveSQLConnection(ctx dutyContext.Context, spec v1.PluginSpec, configItemID string) (*pluginpb.ResolvedConnection, error) {
	if spec.SQLConnection != nil {
		sc := *spec.SQLConnection
		if err := sc.HydrateConnection(ctx); err != nil {
			return nil, fmt.Errorf("sql connection: hydrate plugin spec: %w", err)
		}
		return sqlConnectionToProto(&sc), nil
	}

	if configItemID == "" {
		return nil, fmt.Errorf("sql connection: plugin has no sqlConnection and no config_id was provided")
	}
	if _, err := uuid.Parse(configItemID); err != nil {
		return nil, fmt.Errorf("sql connection: config_id %q is not a UUID", configItemID)
	}
	scraper, err := models.FindScraperByConfigId(ctx.DB(), configItemID)
	if err != nil {
		return nil, fmt.Errorf("sql connection: find scraper for config %s: %w", configItemID, err)
	}
	ref, err := connectionRefFromScraperSpec(scraper.Spec)
	if err != nil {
		return nil, fmt.Errorf("sql connection: scraper %s: %w", scraper.ID, err)
	}
	sc := dutyConn.SQLConnection{ConnectionName: ref}
	if err := sc.HydrateConnection(ctx); err != nil {
		return nil, fmt.Errorf("sql connection: hydrate scraper ref %q: %w", ref, err)
	}
	return sqlConnectionToProto(&sc), nil
}

func sqlConnectionToProto(sc *dutyConn.SQLConnection) *pluginpb.ResolvedConnection {
	props, _ := structpb.NewStruct(map[string]any{"type": sc.Type})
	return &pluginpb.ResolvedConnection{
		Type:       "sql",
		Url:        sc.URL.ValueStatic,
		Username:   sc.Username.ValueStatic,
		Password:   sc.Password.ValueStatic,
		Properties: props,
	}
}

// connectionRefFromScraperSpec parses a ScrapeConfig spec JSON and returns
// the first non-empty `sql[].connection` reference. The shape is fixed by
// the ScrapeConfig CRD: spec.sql is []SQL, SQL embeds Connection, and
// Connection.Connection holds the ref (a UUID or `connection://ns/name`).
func connectionRefFromScraperSpec(specJSON string) (string, error) {
	var spec struct {
		SQL []struct {
			Connection string `json:"connection"`
		} `json:"sql"`
	}
	if err := json.Unmarshal([]byte(specJSON), &spec); err != nil {
		return "", fmt.Errorf("decode spec: %w", err)
	}
	for _, e := range spec.SQL {
		if e.Connection != "" {
			return e.Connection, nil
		}
	}
	return "", fmt.Errorf("no sql.connection set")
}

// _ keeps go vet from complaining about the import-only sync package usage
// in case a future revision drops the cache mutex.
var _ = sync.Mutex{}

// _ retains the v1 import for godoc cross-references; the package is used
// indirectly via registry.Default.Get(...).Spec.
var _ = v1.PluginSpec{}
