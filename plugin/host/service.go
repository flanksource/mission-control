// Package host implements the HostService gRPC server — the back-channel
// that runs in the mission-control process and is dialed by every plugin
// during RegisterPlugin.
//
// All RPCs operate in the calling plugin's identity (matched via the
// peer-info that go-plugin's broker adds to the gRPC context).
package host

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	dutyConn "github.com/flanksource/duty/connection"
	dutyContext "github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
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
	label    string
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
	// plugin process. Keyed by (plugin, type, label, configID).
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
	return pluginpb.FromConfigItem(item)
}

func (s *Service) ListConfigs(ctx context.Context, req *pluginpb.ListConfigsRequest) (*pluginpb.ConfigItemList, error) {
	sel := req.Selector.ToDuty()

	items, err := query.FindConfigsByResourceSelector(s.ctx.Wrap(ctx), int(req.Limit), sel)
	if err != nil {
		return nil, err
	}

	out := &pluginpb.ConfigItemList{}
	for i := range items {
		ci, err := pluginpb.FromConfigItem(items[i])
		if err != nil {
			return nil, err
		}

		out.Items = append(out.Items, ci)
	}

	return out, nil
}

func (s *Service) GetConnection(ctx context.Context, req *pluginpb.GetConnectionRequest) (*pluginpb.ResolvedConnection, error) {
	if req.GetLookup() == nil {
		return nil, fmt.Errorf("connection lookup is required")
	}

	key := connKey{plugin: s.pluginName, typ: req.GetType(), label: req.GetLabel(), configID: req.GetConfigItemId()}
	if cached, ok := s.connCache.Get(key); ok {
		return cached, nil
	}

	entry := registry.Default.Get(s.pluginName)
	if entry == nil {
		return nil, fmt.Errorf("plugin %q is not registered", s.pluginName)
	}

	resolved, err := resolveConnection(s.ctx, entry.Spec, req)
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

// resolveConnection resolves a connection request using one of the plugin
// connection mappings or the scraper that created a config item.
func resolveConnection(ctx dutyContext.Context, spec v1.PluginSpec, req *pluginpb.GetConnectionRequest) (*pluginpb.ResolvedConnection, error) {
	switch lookup := req.GetLookup().(type) {
	case *pluginpb.GetConnectionRequest_Type:
		return resolveConnectionByType(ctx, spec, lookup.Type)
	case *pluginpb.GetConnectionRequest_ConfigItemId:
		return resolveConnectionForConfig(ctx, lookup.ConfigItemId)
	case *pluginpb.GetConnectionRequest_Label:
		return resolveConnectionByLabel(ctx, spec, lookup.Label)
	default:
		return nil, fmt.Errorf("connection lookup is required")
	}
}

func resolveConnectionByType(ctx dutyContext.Context, spec v1.PluginSpec, typ string) (*pluginpb.ResolvedConnection, error) {
	ref, ok := spec.Connections.Types[typ]
	if !ok || ref == "" {
		return nil, fmt.Errorf("plugin did not declare a %s connection mapping", typ)
	}
	return resolveConnectionRef(ctx, ref, typ)
}

func resolveConnectionByLabel(ctx dutyContext.Context, spec v1.PluginSpec, label string) (*pluginpb.ResolvedConnection, error) {
	ref, ok := spec.Connections.Labels[label]
	if !ok || ref == "" {
		return nil, fmt.Errorf("plugin did not declare connection label %q", label)
	}
	return resolveConnectionRef(ctx, ref, "")
}

func resolveConnectionForConfig(ctx dutyContext.Context, configItemID string) (*pluginpb.ResolvedConnection, error) {
	if _, err := uuid.Parse(configItemID); err != nil {
		return nil, fmt.Errorf("connection for config: config_id %q is not a UUID", configItemID)
	}
	scraper, err := models.FindScraperByConfigId(ctx.DB(), configItemID)
	if err != nil {
		return nil, fmt.Errorf("connection for config: find scraper for config %s: %w", configItemID, err)
	}
	return connectionFromScraper(ctx, scraper)
}

func connectionFromScraper(ctx dutyContext.Context, scraper *models.ConfigScraper) (*pluginpb.ResolvedConnection, error) {
	ctx = ctx.WithObject(scraper).WithNamespace(scraper.Namespace)
	if conn, ok, err := connectionFromKubernetesScraper(ctx, scraper.Spec); err != nil {
		return nil, fmt.Errorf("connection for scraper %s: %w", scraper.ID, err)
	} else if ok {
		return connectionToProto(conn, models.ConnectionTypeKubernetes), nil
	}

	if conn, ok, err := connectionFromSQLScraper(ctx, scraper.Spec); err != nil {
		return nil, fmt.Errorf("connection for scraper %s: %w", scraper.ID, err)
	} else if ok {
		return connectionToProto(conn, "sql"), nil
	}

	return nil, fmt.Errorf("connection for scraper %s: unsupported scraper type or no supported connection found", scraper.ID)
}

func connectionFromKubernetesScraper(ctx dutyContext.Context, specJSON string) (*models.Connection, bool, error) {
	var spec struct {
		Kubernetes []dutyConn.KubernetesConnection `json:"kubernetes"`
	}
	if err := json.Unmarshal([]byte(specJSON), &spec); err != nil {
		return nil, false, fmt.Errorf("decode kubernetes scraper spec: %w", err)
	}
	if len(spec.Kubernetes) == 0 {
		return nil, false, nil
	}

	conn := spec.Kubernetes[0]
	if conn.ConnectionName != "" || conn.Kubeconfig != nil && !conn.Kubeconfig.IsEmpty() {
		if _, _, err := conn.Populate(ctx, true); err != nil {
			return nil, true, fmt.Errorf("hydrate kubernetes connection: %w", err)
		}
	}

	kubeconfig := ""
	if conn.Kubeconfig != nil {
		kubeconfig = conn.Kubeconfig.ValueStatic
	}
	if kubeconfig == "" {
		var err error
		kubeconfig, err = readDefaultKubeconfig()
		if err != nil {
			return nil, true, fmt.Errorf("resolve kubeconfig: %w", err)
		}
	}

	return &models.Connection{
		Name:        "scraper-kubernetes",
		Type:        models.ConnectionTypeKubernetes,
		Certificate: kubeconfig,
	}, true, nil
}

func connectionFromSQLScraper(ctx dutyContext.Context, specJSON string) (*models.Connection, bool, error) {
	var spec struct {
		SQL []struct {
			Connection     string `json:"connection"`
			Authentication struct {
				Username types.EnvVar `json:"username"`
				Password types.EnvVar `json:"password"`
			} `json:"auth"`
		} `json:"sql"`
	}
	if err := json.Unmarshal([]byte(specJSON), &spec); err != nil {
		return nil, false, fmt.Errorf("decode sql scraper spec: %w", err)
	}
	if len(spec.SQL) == 0 {
		return nil, false, nil
	}

	for _, sql := range spec.SQL {
		if sql.Connection == "" {
			continue
		}
		conn, err := ctx.HydrateConnectionByURL(sql.Connection)
		if err != nil {
			return nil, true, fmt.Errorf("hydrate sql connection %q: %w", sql.Connection, err)
		}
		if conn == nil {
			return nil, true, fmt.Errorf("sql connection %q not found", sql.Connection)
		}
		if username, err := ctx.GetEnvValueFromCache(sql.Authentication.Username, ctx.GetNamespace()); err != nil {
			return nil, true, fmt.Errorf("hydrate sql username: %w", err)
		} else if username != "" {
			conn.Username = username
		}
		if password, err := ctx.GetEnvValueFromCache(sql.Authentication.Password, ctx.GetNamespace()); err != nil {
			return nil, true, fmt.Errorf("hydrate sql password: %w", err)
		} else if password != "" {
			conn.Password = password
		}
		if conn.Type == "" {
			conn.Type = "sql"
		}
		return conn, true, nil
	}

	return nil, true, fmt.Errorf("no sql.connection set")
}

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
	return "", fmt.Errorf("no default kubeconfig found")
}

func resolveConnectionRef(ctx dutyContext.Context, ref, requestedType string) (*pluginpb.ResolvedConnection, error) {
	conn, err := dutyConn.Get(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("hydrate connection %q: %w", ref, err)
	}
	if requestedType != "" && !connectionTypeMatches(requestedType, conn.Type) {
		return nil, fmt.Errorf("connection %q is %q, not %q", ref, conn.Type, requestedType)
	}
	return connectionToProto(conn, requestedType), nil
}

func connectionTypeMatches(requested, actual string) bool {
	requested = strings.ToLower(strings.ReplaceAll(requested, "-", "_"))
	actual = strings.ToLower(strings.ReplaceAll(actual, "-", "_"))
	if requested == actual {
		return true
	}
	if requested == "sql" {
		switch actual {
		case "postgres", "postgresql", "mysql", "mssql", "sql_server", "sqlserver":
			return true
		}
	}
	return false
}

func connectionToProto(conn *models.Connection, requestedType string) *pluginpb.ResolvedConnection {
	props := map[string]any{
		"type":        conn.Type,
		"name":        conn.Name,
		"namespace":   conn.Namespace,
		"insecureTLS": conn.InsecureTLS,
	}
	for k, v := range conn.Properties {
		props[k] = v
	}
	if conn.URL != "" {
		if _, ok := props["endpoint"]; !ok {
			props["endpoint"] = conn.URL
		}
	}
	if conn.Certificate != "" && connectionTypeMatches("kubernetes", conn.Type) {
		props["kubeconfig"] = conn.Certificate
	}
	pbProps, _ := structpb.NewStruct(props)

	typ := conn.Type
	if requestedType != "" {
		typ = requestedType
	}

	return &pluginpb.ResolvedConnection{
		Type:        typ,
		Url:         conn.URL,
		Username:    conn.Username,
		Password:    conn.Password,
		Certificate: conn.Certificate,
		Token:       connectionToken(conn),
		Properties:  pbProps,
		ExpiresAt:   timestamppb.New(time.Now().Add(connectionCacheTTL)),
	}
}

func connectionToken(conn *models.Connection) string {
	for _, key := range []string{"token", "sessionToken", "session_token"} {
		if token := conn.Properties[key]; token != "" {
			return token
		}
	}
	return ""
}

// connectionRefFromScraperSpec parses a ScrapeConfig spec JSON and returns
// the first non-empty connection reference used by the scraper.
func connectionRefFromScraperSpec(specJSON string) (string, error) {
	var spec any
	decoder := json.NewDecoder(bytes.NewReader([]byte(specJSON)))
	decoder.UseNumber()
	if err := decoder.Decode(&spec); err != nil {
		return "", fmt.Errorf("decode spec: %w", err)
	}
	if ref := firstConnectionRef(spec); ref != "" {
		return ref, nil
	}
	return "", fmt.Errorf("no connection set")
}

func firstConnectionRef(v any) string {
	switch x := v.(type) {
	case map[string]any:
		if ref, ok := x["connection"].(string); ok && ref != "" {
			return ref
		}
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if ref := firstConnectionRef(x[k]); ref != "" {
				return ref
			}
		}
	case []any:
		for _, item := range x {
			if ref := firstConnectionRef(item); ref != "" {
				return ref
			}
		}
	}
	return ""
}

// _ keeps go vet from complaining about the import-only sync package usage
// in case a future revision drops the cache mutex.
var _ = sync.Mutex{}

// _ retains the v1 import for godoc cross-references; the package is used
// indirectly via registry.Default.Get(...).Spec.
var _ = v1.PluginSpec{}
