package main

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	pluginpb "github.com/flanksource/incident-commander/plugin/proto"
	"github.com/flanksource/incident-commander/plugin/sdk"
)

func openPostgres(ctx context.Context, host sdk.HostClient, configItemID string) (*sql.DB, error) {
	if host == nil {
		return nil, fmt.Errorf("host client is unavailable")
	}

	var conn *pluginpb.ResolvedConnection
	var err error
	if configItemID != "" {
		conn, err = host.GetConnectionForConfig(ctx, configItemID)
	} else {
		conn, err = host.GetConnectionByType(ctx, sdk.ConnectionTypePostgres)
	}
	if err != nil {
		return nil, err
	}
	if conn == nil {
		return nil, fmt.Errorf("postgres connection was not resolved")
	}

	dsn, err := postgresDSN(conn)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres connection: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(0)
	db.SetConnMaxLifetime(time.Minute)
	return db, nil
}

func postgresDSN(conn *pluginpb.ResolvedConnection) (string, error) {
	if strings.TrimSpace(conn.Url) != "" {
		return conn.Url, nil
	}

	props := map[string]any{}
	if conn.Properties != nil {
		props = conn.Properties.AsMap()
	}

	host := stringProp(props, "host", "localhost")
	database := stringProp(props, "database", "postgres")
	if !strings.Contains(host, ":") {
		if port := stringProp(props, "port", ""); port != "" {
			host = net.JoinHostPort(host, port)
		}
	}

	u := &url.URL{Scheme: "postgres", Host: host, Path: database}
	if conn.Username != "" || conn.Password != "" {
		u.User = url.UserPassword(conn.Username, conn.Password)
	}

	q := u.Query()
	if boolProp(props, "insecure_tls") || boolProp(props, "insecureTLS") {
		q.Set("sslmode", "disable")
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func stringProp(props map[string]any, key, fallback string) string {
	if v, ok := props[key]; ok {
		s := strings.TrimSpace(fmt.Sprint(v))
		if s != "" {
			return s
		}
	}
	return fallback
}

func boolProp(props map[string]any, key string) bool {
	if v, ok := props[key]; ok {
		switch val := v.(type) {
		case bool:
			return val
		case string:
			return strings.EqualFold(val, "true") || val == "1"
		default:
			return strings.EqualFold(fmt.Sprint(val), "true") || fmt.Sprint(val) == "1"
		}
	}
	return false
}
