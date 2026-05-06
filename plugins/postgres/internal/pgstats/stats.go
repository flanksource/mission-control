package pgstats

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
)

type Response struct {
	CapturedAt  time.Time        `json:"capturedAt"`
	Instance    *InstanceStats   `json:"instance,omitempty"`
	Connections *ConnectionStats `json:"connections,omitempty"`
	Database    *DatabaseStats   `json:"database,omitempty"`
	Warnings    []string         `json:"warnings,omitempty"`
}

type InstanceStats struct {
	ServerName     string    `json:"serverName,omitempty"`
	DatabaseName   string    `json:"databaseName,omitempty"`
	ProductVersion string    `json:"productVersion,omitempty"`
	MaxConnections int       `json:"maxConnections,omitempty"`
	StartedAt      time.Time `json:"startedAt,omitempty"`
	UptimeSeconds  int64     `json:"uptimeSeconds,omitempty"`
}

type ConnectionStats struct {
	Total   int `json:"total"`
	Active  int `json:"active"`
	Idle    int `json:"idle"`
	Waiting int `json:"waiting"`
	Max     int `json:"max"`
}

type DatabaseStats struct {
	SizeBytes       int64   `json:"sizeBytes"`
	Transactions    int64   `json:"transactions"`
	Rollbacks       int64   `json:"rollbacks"`
	Deadlocks       int64   `json:"deadlocks"`
	TempBytes       int64   `json:"tempBytes"`
	CacheHitPercent float64 `json:"cacheHitPercent"`
	TuplesReturned  int64   `json:"tuplesReturned"`
	TuplesFetched   int64   `json:"tuplesFetched"`
	TuplesInserted  int64   `json:"tuplesInserted"`
	TuplesUpdated   int64   `json:"tuplesUpdated"`
	TuplesDeleted   int64   `json:"tuplesDeleted"`
	Conflicts       int64   `json:"conflicts"`
}

func Collect(ctx context.Context, db *gorm.DB) (Response, error) {
	resp := Response{CapturedAt: time.Now()}
	if instance, err := collectInstance(ctx, db); err != nil {
		resp.Warnings = append(resp.Warnings, "instance unavailable: "+err.Error())
	} else {
		resp.Instance = &instance
	}
	if connections, err := collectConnections(ctx, db, resp.Instance); err != nil {
		resp.Warnings = append(resp.Warnings, "connections unavailable: "+err.Error())
	} else {
		resp.Connections = &connections
	}
	if database, err := collectDatabase(ctx, db); err != nil {
		resp.Warnings = append(resp.Warnings, "database stats unavailable: "+err.Error())
	} else {
		resp.Database = &database
	}
	if len(resp.Warnings) == 0 {
		resp.Warnings = nil
	}
	return resp, nil
}

func collectInstance(ctx context.Context, db *gorm.DB) (InstanceStats, error) {
	var row struct {
		ServerName     *string    `gorm:"column:server_name"`
		DatabaseName   *string    `gorm:"column:database_name"`
		ProductVersion *string    `gorm:"column:product_version"`
		MaxConnections *int       `gorm:"column:max_connections"`
		StartedAt      *time.Time `gorm:"column:started_at"`
	}
	if err := db.WithContext(ctx).Raw(`
SELECT
  COALESCE(inet_server_addr()::text, '') AS server_name,
  current_database() AS database_name,
  version() AS product_version,
  current_setting('max_connections')::int AS max_connections,
  pg_postmaster_start_time() AS started_at
`).Scan(&row).Error; err != nil {
		return InstanceStats{}, err
	}
	out := InstanceStats{
		ServerName:     strDeref(row.ServerName),
		DatabaseName:   strDeref(row.DatabaseName),
		ProductVersion: strDeref(row.ProductVersion),
		MaxConnections: intDeref(row.MaxConnections),
	}
	if row.StartedAt != nil {
		out.StartedAt = *row.StartedAt
		out.UptimeSeconds = int64(time.Since(*row.StartedAt).Seconds())
	}
	return out, nil
}

func collectConnections(ctx context.Context, db *gorm.DB, instance *InstanceStats) (ConnectionStats, error) {
	var row struct {
		Total   *int `gorm:"column:total"`
		Active  *int `gorm:"column:active"`
		Idle    *int `gorm:"column:idle"`
		Waiting *int `gorm:"column:waiting"`
	}
	if err := db.WithContext(ctx).Raw(`
SELECT
  count(*)::int AS total,
  count(*) FILTER (WHERE state = 'active')::int AS active,
  count(*) FILTER (WHERE state = 'idle')::int AS idle,
  count(*) FILTER (WHERE wait_event_type IS NOT NULL)::int AS waiting
FROM pg_stat_activity
`).Scan(&row).Error; err != nil {
		return ConnectionStats{}, err
	}
	out := ConnectionStats{
		Total:   intDeref(row.Total),
		Active:  intDeref(row.Active),
		Idle:    intDeref(row.Idle),
		Waiting: intDeref(row.Waiting),
	}
	if instance != nil {
		out.Max = instance.MaxConnections
	}
	return out, nil
}

func collectDatabase(ctx context.Context, db *gorm.DB) (DatabaseStats, error) {
	var row struct {
		SizeBytes       *int64   `gorm:"column:size_bytes"`
		Transactions    *int64   `gorm:"column:transactions"`
		Rollbacks       *int64   `gorm:"column:rollbacks"`
		Deadlocks       *int64   `gorm:"column:deadlocks"`
		TempBytes       *int64   `gorm:"column:temp_bytes"`
		CacheHitPercent *float64 `gorm:"column:cache_hit_percent"`
		TuplesReturned  *int64   `gorm:"column:tuples_returned"`
		TuplesFetched   *int64   `gorm:"column:tuples_fetched"`
		TuplesInserted  *int64   `gorm:"column:tuples_inserted"`
		TuplesUpdated   *int64   `gorm:"column:tuples_updated"`
		TuplesDeleted   *int64   `gorm:"column:tuples_deleted"`
		Conflicts       *int64   `gorm:"column:conflicts"`
	}
	if err := db.WithContext(ctx).Raw(`
SELECT
  pg_database_size(datname) AS size_bytes,
  xact_commit AS transactions,
  xact_rollback AS rollbacks,
  deadlocks,
  temp_bytes,
  CASE WHEN blks_hit + blks_read = 0 THEN 0
       ELSE (blks_hit::float8 / (blks_hit + blks_read)) * 100 END AS cache_hit_percent,
  tup_returned AS tuples_returned,
  tup_fetched AS tuples_fetched,
  tup_inserted AS tuples_inserted,
  tup_updated AS tuples_updated,
  tup_deleted AS tuples_deleted,
  conflicts
FROM pg_stat_database
WHERE datname = current_database()
`).Scan(&row).Error; err != nil {
		return DatabaseStats{}, fmt.Errorf("pg_stat_database: %w", err)
	}
	return DatabaseStats{
		SizeBytes:       int64Deref(row.SizeBytes),
		Transactions:    int64Deref(row.Transactions),
		Rollbacks:       int64Deref(row.Rollbacks),
		Deadlocks:       int64Deref(row.Deadlocks),
		TempBytes:       int64Deref(row.TempBytes),
		CacheHitPercent: float64Deref(row.CacheHitPercent),
		TuplesReturned:  int64Deref(row.TuplesReturned),
		TuplesFetched:   int64Deref(row.TuplesFetched),
		TuplesInserted:  int64Deref(row.TuplesInserted),
		TuplesUpdated:   int64Deref(row.TuplesUpdated),
		TuplesDeleted:   int64Deref(row.TuplesDeleted),
		Conflicts:       int64Deref(row.Conflicts),
	}, nil
}

func strDeref(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func intDeref(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}

func int64Deref(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}

func float64Deref(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}
