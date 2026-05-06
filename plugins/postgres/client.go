package main

import (
	"context"
	databasesql "database/sql"
	"fmt"
	"net/url"
	"strings"
	"sync"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/flanksource/incident-commander/plugin/sdk"
)

const PostgreSQLDatabaseType = "PostgreSQL::Database"

type resolved struct {
	DB            *gorm.DB
	BoundDatabase string
}

type connectionCache struct {
	mu      sync.Mutex
	entries map[string]*resolved
}

func (c *connectionCache) For(ctx context.Context, host sdk.HostClient, configItemID, database string) (*resolved, error) {
	if configItemID == "" {
		return nil, fmt.Errorf("config_item_id is required to resolve a SQL connection")
	}
	if host == nil {
		return nil, fmt.Errorf("no host client (HTTP handlers must call operations to access the SQL connection)")
	}

	bound := boundDatabase(ctx, host, configItemID)
	if bound != "" {
		database = bound
	}
	key := configItemID + "\x00" + database
	if existing, ok := c.lookup(key); ok {
		return existing, nil
	}

	conn, err := host.GetConnection(ctx, "sql", configItemID)
	if err != nil {
		return nil, fmt.Errorf("get sql connection: %w", err)
	}
	if conn == nil || conn.Url == "" {
		return nil, fmt.Errorf("host returned an empty SQL connection url for %s", configItemID)
	}
	connType := "postgres"
	if conn.Properties != nil {
		if t, ok := conn.Properties.AsMap()["type"].(string); ok && t != "" {
			connType = t
		}
	}
	if connType != "postgres" {
		return nil, fmt.Errorf("postgres plugin requires a postgres SQL connection, got %q", connType)
	}

	connURL := conn.Url
	if database != "" {
		updated, err := withDefaultDatabase(connURL, database)
		if err != nil {
			return nil, fmt.Errorf("scope connection to database %q: %w", database, err)
		}
		connURL = updated
	}
	db, err := openGorm(ctx, connURL)
	if err != nil {
		return nil, err
	}
	r := &resolved{DB: db, BoundDatabase: bound}
	c.store(key, r)
	return r, nil
}

func boundDatabase(ctx context.Context, host sdk.HostClient, configItemID string) string {
	item, err := host.GetConfigItem(ctx, configItemID)
	if err != nil || item == nil || item.Type != PostgreSQLDatabaseType {
		return ""
	}
	return item.Name
}

func (c *connectionCache) lookup(k string) (*resolved, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = map[string]*resolved{}
	}
	v, ok := c.entries[k]
	return v, ok
}

func (c *connectionCache) store(k string, v *resolved) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = map[string]*resolved{}
	}
	c.entries[k] = v
}

func openGorm(ctx context.Context, rawURL string) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(rawURL), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("unwrap postgres: %w", err)
	}
	if err := pingSQL(ctx, sqlDB); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return db, nil
}

func pingSQL(ctx context.Context, db *databasesql.DB) error {
	return db.PingContext(ctx)
}

func withDefaultDatabase(raw, database string) (string, error) {
	u, err := url.Parse(raw)
	if err == nil && u.Scheme != "" {
		u.Path = "/" + database
		q := u.Query()
		q.Del("dbname")
		u.RawQuery = q.Encode()
		return u.String(), nil
	}
	return withKeywordDatabase(raw, database)
}

func withKeywordDatabase(raw, database string) (string, error) {
	parts := strings.Fields(raw)
	if len(parts) == 0 {
		return "", fmt.Errorf("empty postgres connection string")
	}
	replaced := false
	for i, part := range parts {
		if strings.HasPrefix(part, "dbname=") {
			parts[i] = "dbname=" + database
			replaced = true
		}
	}
	if !replaced {
		parts = append(parts, "dbname="+database)
	}
	return strings.Join(parts, " "), nil
}
