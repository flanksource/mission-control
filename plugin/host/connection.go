package host

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	dutyConn "github.com/flanksource/duty/connection"
	dutyContext "github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	v1 "github.com/flanksource/incident-commander/api/v1"
	pluginpb "github.com/flanksource/incident-commander/plugin/proto"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

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
