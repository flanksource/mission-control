package host

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	dutyAPI "github.com/flanksource/duty/api"
	dutyConn "github.com/flanksource/duty/connection"
	dutyContext "github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/types"
	pluginpb "github.com/flanksource/incident-commander/plugin/proto"
	"github.com/flanksource/incident-commander/plugin/registry"
	"google.golang.org/protobuf/proto"
)

func (s *Service) getConnectionByRef(ctx dutyContext.Context, ref string) (*pluginpb.ResolvedConnection, error) {
	conn, err := dutyContext.FindConnectionByURL(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("find connection %q: %w", ref, err)
	} else if conn == nil {
		return nil, ctx.Oops().Code(dutyAPI.ENOTFOUND).Errorf("connection %q not found", ref)
	}

	attr := models.ABACAttribute{Connection: *conn}
	if !dutyRBAC.HasPermission(ctx, ctx.Subject(), &attr, policy.ActionRead) {
		return nil, ctx.Oops().Code(dutyAPI.EFORBIDDEN).Errorf("access denied to %s, `read` permission required on %s", ctx.Subject(), ref)
	}

	key := connKey{connectionID: conn.ID.String()}
	if cached, ok := s.connCache.Get(key); ok {
		return cloneResolvedConnection(cached), nil
	}

	hydrated, err := dutyContext.HydrateConnection(ctx, conn)
	if err != nil {
		return nil, fmt.Errorf("hydrate connection %q: %w", ref, err)
	}

	resolved := pluginpb.ConnectionToProto(hydrated)
	s.connCache.Add(key, resolved)
	return cloneResolvedConnection(resolved), nil
}

func (s *Service) getConnectionForConfig(ctx dutyContext.Context, configItemID string) (*pluginpb.ResolvedConnection, error) {
	scraper, err := models.FindScraperByConfigId(ctx.DB(), configItemID)
	if err != nil {
		return nil, fmt.Errorf("connection for config: find scraper for config %s: %w", configItemID, err)
	}

	ctx = ctx.WithNamespace(scraper.Namespace)

	if conn, ok, err := s.connectionFromKubernetesScraper(ctx, scraper.Spec); err != nil {
		return nil, fmt.Errorf("connection for scraper %s: %w", scraper.ID, err)
	} else if ok {
		return conn, nil
	}

	if conn, ok, err := s.connectionFromSQLScraper(ctx, scraper.Spec); err != nil {
		return nil, fmt.Errorf("connection for scraper %s: %w", scraper.ID, err)
	} else if ok {
		return conn, nil
	}

	return nil, fmt.Errorf("connection for scraper %s: unsupported scraper type or no supported connection found", scraper.ID)
}

func (s *Service) connectionFromKubernetesScraper(ctx dutyContext.Context, specJSON string) (*pluginpb.ResolvedConnection, bool, error) {
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
	if conn.ConnectionName != "" {
		resolved, err := s.getConnectionByRef(ctx, conn.ConnectionName)
		return resolved, true, err
	}

	kubeconfig := ""
	if conn.Kubeconfig != nil && !conn.Kubeconfig.IsEmpty() {
		var err error
		kubeconfig, err = ctx.GetEnvValueFromCache(*conn.Kubeconfig, ctx.GetNamespace())
		if err != nil {
			return nil, true, fmt.Errorf("hydrate kubernetes connection: %w", err)
		}
	}

	if kubeconfig == "" {
		var err error
		kubeconfig, err = readDefaultKubeconfig()
		if err != nil {
			return nil, true, fmt.Errorf("resolve kubeconfig: %w", err)
		}
	}

	return pluginpb.ConnectionToProto(&models.Connection{
		Name:        "scraper-kubernetes",
		Type:        models.ConnectionTypeKubernetes,
		Certificate: kubeconfig,
	}), true, nil
}

func (s *Service) connectionFromSQLScraper(ctx dutyContext.Context, specJSON string) (*pluginpb.ResolvedConnection, bool, error) {
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

		if dutyContext.IsValidConnectionURL(sql.Connection) {
			conn, err := s.getConnectionByRef(ctx, sql.Connection)
			if err != nil {
				return nil, true, fmt.Errorf("hydrate sql connection %q: %w", sql.Connection, err)
			}
			return conn, true, nil
		}

		conn := parseRawSQLConnection(sql.Connection)
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

		return pluginpb.ConnectionToProto(conn), true, nil
	}

	return nil, true, fmt.Errorf("no sql.connection set")
}

func cloneResolvedConnection(conn *pluginpb.ResolvedConnection) *pluginpb.ResolvedConnection {
	if conn == nil {
		return nil
	}
	return proto.Clone(conn).(*pluginpb.ResolvedConnection)
}

func parseRawSQLConnection(connection string) *models.Connection {
	conn := &models.Connection{URL: connection}
	parsed, err := url.Parse(connection)
	if err != nil || parsed.Scheme == "" || parsed.User == nil {
		return conn
	}

	conn.Username = parsed.User.Username()
	conn.Password, _ = parsed.User.Password()
	parsed.User = nil
	conn.URL = parsed.String()
	return conn
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

func pluginRBACSubject(entry *registry.Entry) string {
	if entry == nil {
		return "plugin:/"
	}
	// Plugin permissions use namespace/name instead of UUID because plugins are
	// persisted as CRDs/registry entries, not database rows whose IDs can be
	// resolved when Permission CRDs are converted into Casbin rules.
	return "plugin:" + entry.Namespace + "/" + entry.Name
}
