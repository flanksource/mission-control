package pgslow

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"
)

type Response struct {
	Available bool    `json:"available"`
	Warning   string  `json:"warning,omitempty"`
	Queries   []Query `json:"queries,omitempty"`
}

type InstallResult struct {
	Installed               bool   `json:"installed"`
	ExtensionVersion        string `json:"extensionVersion,omitempty"`
	SharedPreloadConfigured bool   `json:"sharedPreloadConfigured"`
	SharedPreloadLibraries  string `json:"sharedPreloadLibraries,omitempty"`
	Warning                 string `json:"warning,omitempty"`
}

type Query struct {
	User              string  `json:"user,omitempty"`
	Database          string  `json:"database,omitempty"`
	Query             string  `json:"query"`
	Calls             int64   `json:"calls"`
	TotalExecTimeMS   float64 `json:"totalExecTimeMs"`
	MeanExecTimeMS    float64 `json:"meanExecTimeMs"`
	Rows              int64   `json:"rows"`
	SharedBlocksHit   int64   `json:"sharedBlocksHit"`
	SharedBlocksRead  int64   `json:"sharedBlocksRead"`
	TempBlocksWritten int64   `json:"tempBlocksWritten"`
}

type row struct {
	User              *string  `gorm:"column:user_name"`
	Database          *string  `gorm:"column:database_name"`
	Query             string   `gorm:"column:query"`
	Calls             *int64   `gorm:"column:calls"`
	TotalExecTimeMS   *float64 `gorm:"column:total_exec_time_ms"`
	MeanExecTimeMS    *float64 `gorm:"column:mean_exec_time_ms"`
	Rows              *int64   `gorm:"column:rows"`
	SharedBlocksHit   *int64   `gorm:"column:shared_blks_hit"`
	SharedBlocksRead  *int64   `gorm:"column:shared_blks_read"`
	TempBlocksWritten *int64   `gorm:"column:temp_blks_written"`
}

func List(ctx context.Context, db *gorm.DB, limit int) (Response, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var exists bool
	if err := db.WithContext(ctx).Raw(`SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pg_stat_statements')`).Scan(&exists).Error; err != nil {
		return Response{}, fmt.Errorf("check pg_stat_statements: %w", err)
	}
	if !exists {
		return Response{Available: false, Warning: "pg_stat_statements is not installed in this database"}, nil
	}

	var rows []row
	if err := db.WithContext(ctx).Raw(`
SELECT r.rolname AS user_name, d.datname AS database_name, s.query, s.calls,
       s.total_exec_time AS total_exec_time_ms,
       s.mean_exec_time AS mean_exec_time_ms,
       s.rows, s.shared_blks_hit, s.shared_blks_read, s.temp_blks_written
FROM pg_stat_statements s
LEFT JOIN pg_roles r ON r.oid = s.userid
LEFT JOIN pg_database d ON d.oid = s.dbid
WHERE d.datname = current_database()
ORDER BY s.total_exec_time DESC
LIMIT ?
`, limit).Scan(&rows).Error; err != nil {
		return Response{Available: false, Warning: "pg_stat_statements is installed but could not be queried: " + err.Error()}, nil
	}
	out := make([]Query, 0, len(rows))
	for _, r := range rows {
		out = append(out, Query{
			User:              strDeref(r.User),
			Database:          strDeref(r.Database),
			Query:             r.Query,
			Calls:             int64Deref(r.Calls),
			TotalExecTimeMS:   float64Deref(r.TotalExecTimeMS),
			MeanExecTimeMS:    float64Deref(r.MeanExecTimeMS),
			Rows:              int64Deref(r.Rows),
			SharedBlocksHit:   int64Deref(r.SharedBlocksHit),
			SharedBlocksRead:  int64Deref(r.SharedBlocksRead),
			TempBlocksWritten: int64Deref(r.TempBlocksWritten),
		})
	}
	return Response{Available: true, Queries: out}, nil
}

func Install(ctx context.Context, db *gorm.DB) (InstallResult, error) {
	if err := db.WithContext(ctx).Exec(`CREATE EXTENSION IF NOT EXISTS pg_stat_statements`).Error; err != nil {
		return InstallResult{}, fmt.Errorf("create extension pg_stat_statements: %w", err)
	}

	var row struct {
		Version *string `gorm:"column:extversion"`
	}
	if err := db.WithContext(ctx).Raw(`SELECT extversion FROM pg_extension WHERE extname = 'pg_stat_statements'`).Scan(&row).Error; err != nil {
		return InstallResult{}, fmt.Errorf("verify pg_stat_statements extension: %w", err)
	}

	result := InstallResult{
		Installed:        row.Version != nil,
		ExtensionVersion: strDeref(row.Version),
	}
	var preload string
	if err := db.WithContext(ctx).Raw(`SELECT current_setting('shared_preload_libraries', true)`).Scan(&preload).Error; err == nil {
		result.SharedPreloadLibraries = preload
		result.SharedPreloadConfigured = hasSharedPreload(preload, "pg_stat_statements")
	}
	if !result.SharedPreloadConfigured {
		result.Warning = "pg_stat_statements extension is installed, but shared_preload_libraries does not include pg_stat_statements; query statistics may stay unavailable until Postgres is configured and restarted"
	}
	return result, nil
}

func strDeref(v *string) string {
	if v == nil {
		return ""
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

func hasSharedPreload(value, target string) bool {
	for _, part := range strings.Split(value, ",") {
		if strings.TrimSpace(part) == target {
			return true
		}
	}
	return false
}
