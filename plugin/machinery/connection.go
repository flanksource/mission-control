package machinery

import (
	"encoding/json"
	"fmt"
	"net/url"

	dutyAPI "github.com/flanksource/duty/api"
	dutyConn "github.com/flanksource/duty/connection"
	dutyContext "github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	pluginpb "github.com/flanksource/incident-commander/plugin"
	"github.com/flanksource/incident-commander/plugin/api"
	"github.com/flanksource/incident-commander/rbac"
	"github.com/flanksource/incident-commander/rbac/adapter"
)

// getConnectionByRef resolves a connection for a plugin, by its connection://<namespace>/<name> URL or its id.
//
// It's checked as the plugin (specs/authorization/roles.md, Section 4.4): connection:use returns the connection with its
// credentials, read returns it with them masked, and neither refuses it.
func (s *Service) getConnectionByRef(ctx dutyContext.Context, ref string) (*api.ResolvedConnection, error) {
	conn, err := dutyContext.FindConnectionByURL(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("find connection %q: %w", ref, err)
	} else if conn == nil {
		return nil, ctx.Oops().Code(dutyAPI.ENOTFOUND).Errorf("connection %q not found", ref)
	}

	if !rbac.CanUseConnection(ctx, ctx.Subject(), *conn) {
		if rbac.CanReadConnection(ctx, ctx.Subject(), *conn) {
			masked := db.MaskConnection(*conn)
			return pluginpb.ConnectionToProto(&masked), nil
		}
		return nil, ctx.Oops().Code(dutyAPI.EFORBIDDEN).Errorf("access denied to %s, `read` or `%s` permission required on %s", ctx.Subject(), adapter.ActionConnectionUse, ref)
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

func (s *Service) getConnectionByID(ctx dutyContext.Context, connectionID string) (*api.ResolvedConnection, error) {
	if connectionID == "" {
		return nil, fmt.Errorf("connection id is required")
	} else if _, err := uuid.Parse(connectionID); err != nil {
		return nil, ctx.Oops().Code(dutyAPI.EINVALID).Errorf("invalid connection id %q", connectionID)
	}

	return s.getConnectionByRef(ctx, connectionID)
}

func (s *Service) getConnectionForConfig(ctx dutyContext.Context, configItemID string) (*api.ResolvedConnection, error) {
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

func (s *Service) connectionFromKubernetesScraper(ctx dutyContext.Context, specJSON string) (*api.ResolvedConnection, bool, error) {
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

	return nil, true, errInlineCredentials(ctx, "kubeconfig")
}

func (s *Service) connectionFromSQLScraper(ctx dutyContext.Context, specJSON string) (*api.ResolvedConnection, bool, error) {
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
		if conn.Username != "" || conn.Password != "" || !sql.Authentication.Username.IsEmpty() || !sql.Authentication.Password.IsEmpty() {
			return nil, true, errInlineCredentials(ctx, "sql credentials")
		}

		return pluginpb.ConnectionToProto(conn), true, nil
	}

	return nil, true, fmt.Errorf("no sql.connection set")
}

func cloneResolvedConnection(conn *api.ResolvedConnection) *api.ResolvedConnection {
	if conn == nil {
		return nil
	}
	return proto.Clone(conn).(*api.ResolvedConnection)
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

// errInlineCredentials refuses credentials a scraper holds itself, instead of referencing a connection.
// A plugin is only granted credentials by connection:use on a connection (specs/authorization/roles.md, Section 4.4),
// so nothing can grant it these.
func errInlineCredentials(ctx dutyContext.Context, what string) error {
	return ctx.Oops().Code(dutyAPI.EFORBIDDEN).
		Errorf("access denied to %s: the scraper's %s aren't held by a connection, so they can't be granted to a plugin. Reference a connection from the scraper and grant the plugin `%s` on it", ctx.Subject(), what, adapter.ActionConnectionUse)
}

func pluginRBACSubject(entry *pluginpb.Entry) string {
	if entry == nil {
		return v1.PluginSubject("", "")
	}
	// Plugin permissions use namespace/name instead of UUID because plugins are
	// persisted as CRDs/registry entries, not database rows whose IDs can be
	// resolved when Permission CRDs are converted into Casbin rules.
	return v1.PluginSubject(entry.Namespace, entry.Name)
}
