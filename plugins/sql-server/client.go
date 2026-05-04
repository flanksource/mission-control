package main

import (
	"context"
	databasesql "database/sql"
	"fmt"
	"net/url"
	"sync"

	_ "github.com/microsoft/go-mssqldb"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlserver"
	"gorm.io/gorm"

	"github.com/flanksource/incident-commander/plugin/sdk"
)

// MSSQLDatabaseType is the catalog item type that pins the plugin to a single
// database. When the user opens the iframe on an item of this type, every
// query must be constrained to it (USE [name] before each statement).
const MSSQLDatabaseType = "MSSQL::Database"

// resolved is what every operation handler unwraps from the cache: the gorm
// connection plus the optional database the iframe is scoped to. When
// BoundDatabase is set, ops MUST pass it through to sqlquery.Options.Database
// (or use it as the default for filters like Processes/Stats).
type resolved struct {
	DB            *gorm.DB
	BoundDatabase string
}

// connectionCache memoises one resolved entry per configItemID so repeated
// operations on the same catalog item reuse the pool and the bound database
// lookup. The host's GetConnection returns the resolved connection URL — we
// open a sql.DB against it lazily and wrap with gorm.
type connectionCache struct {
	mu      sync.Mutex
	entries map[string]*resolved
}

// For returns a resolved entry for the catalog item at configItemID. The host
// resolves the SQL connection via resolveSQLConnection in plugin/host.
// Additionally, when the catalog item itself is an MSSQL::Database, its name
// becomes the bound database — every subsequent operation must scope to it.
func (c *connectionCache) For(ctx context.Context, host sdk.HostClient, configItemID string) (*resolved, error) {
	if configItemID == "" {
		return nil, fmt.Errorf("config_item_id is required to resolve a SQL connection")
	}
	if existing, ok := c.lookup(configItemID); ok {
		return existing, nil
	}
	if host == nil {
		return nil, fmt.Errorf("no host client (HTTP handlers must call operations to access the SQL connection)")
	}
	conn, err := host.GetConnection(ctx, "sql", configItemID)
	if err != nil {
		return nil, fmt.Errorf("get sql connection: %w", err)
	}
	if conn == nil || conn.Url == "" {
		return nil, fmt.Errorf("host returned an empty SQL connection url for %s", configItemID)
	}
	connType := "sql_server"
	if conn.Properties != nil {
		if t, ok := conn.Properties.AsMap()["type"].(string); ok && t != "" {
			connType = t
		}
	}
	bound := boundDatabase(ctx, host, configItemID)
	connURL := conn.Url
	if bound != "" {
		// Bake the database into the connection URL so every pooled
		// connection lands in the right database. USE on a single session
		// won't carry across the pool — gorm hands a different underlying
		// *sql.Conn to each query.
		updated, err := withDefaultDatabase(connType, connURL, bound)
		if err != nil {
			return nil, fmt.Errorf("scope connection to database %q: %w", bound, err)
		}
		connURL = updated
	}
	db, err := openGorm(ctx, connType, connURL)
	if err != nil {
		return nil, err
	}
	r := &resolved{DB: db, BoundDatabase: bound}
	c.store(configItemID, r)
	return r, nil
}

// boundDatabase returns the database name when the catalog item is itself
// MSSQL::Database. It returns "" silently for any other type or on lookup
// failure — being unable to look up the config item just means we run
// unconstrained, which is the same behaviour as before this feature.
func boundDatabase(ctx context.Context, host sdk.HostClient, configItemID string) string {
	item, err := host.GetConfigItem(ctx, configItemID)
	if err != nil || item == nil {
		return ""
	}
	if item.Type != MSSQLDatabaseType {
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

func openGorm(ctx context.Context, connType, url string) (*gorm.DB, error) {
	dialector, err := dialectorFor(connType, url)
	if err != nil {
		return nil, err
	}
	db, err := gorm.Open(dialector, &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", connType, err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("unwrap %s: %w", connType, err)
	}
	if err := pingSQL(ctx, sqlDB); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("ping %s: %w", connType, err)
	}
	return db, nil
}

func dialectorFor(connType, url string) (gorm.Dialector, error) {
	switch connType {
	case "sql_server", "":
		return sqlserver.Open(url), nil
	case "postgres":
		return postgres.Open(url), nil
	case "mysql":
		return mysql.Open(url), nil
	default:
		return nil, fmt.Errorf("unsupported sql connection type %q", connType)
	}
}

func pingSQL(ctx context.Context, db *databasesql.DB) error {
	return db.PingContext(ctx)
}

// withDefaultDatabase rewrites a connection URL so the driver opens every
// pooled connection in the named database. It supports the URL form
// ("sqlserver://host/inst?database=foo", "postgres://...", "mysql://...");
// non-URL "key=value;..." DSNs are returned unchanged with the database
// appended. Existing database= / dbname= parameters are overwritten.
func withDefaultDatabase(connType, raw, database string) (string, error) {
	switch connType {
	case "postgres":
		return setURLParam(raw, "dbname", database)
	default:
		// sqlserver, mysql, and the empty default all accept ?database=
		return setURLParam(raw, "database", database)
	}
}

func setURLParam(raw, key, value string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" {
		// Not a URL — append (or overwrite) ;key=value to the DSN.
		return raw, fmt.Errorf("connection URL is not parseable as URL; cannot scope to database via %q", key)
	}
	q := u.Query()
	q.Set(key, value)
	u.RawQuery = q.Encode()
	return u.String(), nil
}
