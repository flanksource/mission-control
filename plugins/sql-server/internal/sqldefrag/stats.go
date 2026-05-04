package sqldefrag

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

type StatsOptions struct {
	Database            string `json:"database,omitempty"`
	MaintenanceDatabase string `json:"maintenanceDatabase,omitempty"`
	Table               string `json:"table,omitempty"`
}

type StatsView struct {
	Databases []DatabaseStats `json:"databases"`
	Tables    []TableStats    `json:"tables"`
	Indexes   []IndexStats    `json:"indexes"`
}

type DatabaseStats struct {
	Database         string     `json:"database" gorm:"column:database_name"`
	TotalIndexes     int64      `json:"totalIndexes" gorm:"column:total_indexes"`
	DoneIndexes      int64      `json:"doneIndexes" gorm:"column:done_indexes"`
	TotalStatistics  int64      `json:"totalStatistics" gorm:"column:total_statistics"`
	DoneStatistics   int64      `json:"doneStatistics" gorm:"column:done_statistics"`
	TotalRows        int64      `json:"totalRows" gorm:"column:total_rows"`
	TotalPages       int64      `json:"totalPages" gorm:"column:total_pages"`
	MaxFragmentation *float64   `json:"maxFragmentation,omitempty" gorm:"column:max_fragmentation"`
	LastScan         *time.Time `json:"lastScan,omitempty" gorm:"column:last_scan"`
	LastDefrag       *time.Time `json:"lastDefrag,omitempty" gorm:"column:last_defrag"`
	LastStatsUpdate  *time.Time `json:"lastStatsUpdate,omitempty" gorm:"column:last_stats_update"`
}

type TableStats struct {
	Database         string     `json:"database" gorm:"column:database_name"`
	Schema           string     `json:"schema,omitempty" gorm:"column:schema_name"`
	ObjectName       string     `json:"objectName" gorm:"column:object_name"`
	TableName        string     `json:"tableName" gorm:"column:table_name"`
	TotalIndexes     int64      `json:"totalIndexes" gorm:"column:total_indexes"`
	DoneIndexes      int64      `json:"doneIndexes" gorm:"column:done_indexes"`
	TotalStatistics  int64      `json:"totalStatistics" gorm:"column:total_statistics"`
	DoneStatistics   int64      `json:"doneStatistics" gorm:"column:done_statistics"`
	TotalRows        int64      `json:"totalRows" gorm:"column:total_rows"`
	MaxFragmentation *float64   `json:"maxFragmentation,omitempty" gorm:"column:max_fragmentation"`
	TotalPages       int64      `json:"totalPages" gorm:"column:total_pages"`
	LastScan         *time.Time `json:"lastScan,omitempty" gorm:"column:last_scan"`
	LastDefrag       *time.Time `json:"lastDefrag,omitempty" gorm:"column:last_defrag"`
	LastStatsUpdate  *time.Time `json:"lastStatsUpdate,omitempty" gorm:"column:last_stats_update"`
}

type IndexStats struct {
	Database       string     `json:"database" gorm:"column:database_name"`
	Schema         string     `json:"schema,omitempty" gorm:"column:schema_name"`
	ObjectName     string     `json:"objectName" gorm:"column:object_name"`
	TableName      string     `json:"tableName" gorm:"column:table_name"`
	Name           string     `json:"name" gorm:"column:name"`
	Kind           string     `json:"kind" gorm:"column:kind"`
	Partition      *int       `json:"partition,omitempty" gorm:"column:partition_number"`
	Done           bool       `json:"done" gorm:"column:done"`
	Fragmentation  *float64   `json:"fragmentation,omitempty" gorm:"column:fragmentation"`
	PageCount      *int64     `json:"pageCount,omitempty" gorm:"column:page_count"`
	RecordCount    *int64     `json:"recordCount,omitempty" gorm:"column:record_count"`
	RangeScanCount *int64     `json:"rangeScanCount,omitempty" gorm:"column:range_scan_count"`
	ScanDate       *time.Time `json:"scanDate,omitempty" gorm:"column:scan_date"`
	CompletedAt    *time.Time `json:"completedAt,omitempty" gorm:"column:completed_at"`
}

func Stats(ctx context.Context, db *gorm.DB, opts StatsOptions) (StatsView, error) {
	database, err := resolveDatabase(ctx, db, opts.Database)
	if err != nil {
		return StatsView{}, err
	}
	maintenanceDB := normalizeMaintenanceDatabase(opts.MaintenanceDatabase)
	hasIndexes := tableExists(ctx, db, maintenanceDB, "tbl_AdaptiveIndexDefrag_Working")
	hasStats := tableExists(ctx, db, maintenanceDB, "tbl_AdaptiveIndexDefrag_Stats_Working")
	if !hasIndexes && !hasStats {
		return StatsView{}, nil
	}

	view := StatsView{}
	if err := scanDatabaseStats(ctx, db, maintenanceDB, database, hasIndexes, hasStats, &view.Databases); err != nil {
		return view, err
	}
	if err := scanTableStats(ctx, db, maintenanceDB, database, opts.Table, hasIndexes, hasStats, &view.Tables); err != nil {
		return view, err
	}
	if err := scanIndexStats(ctx, db, maintenanceDB, database, opts.Table, hasIndexes, hasStats, &view.Indexes); err != nil {
		return view, err
	}
	return view, nil
}

func scanDatabaseStats(ctx context.Context, db *gorm.DB, maintenanceDB, database string, hasIndexes, hasStats bool, out *[]DatabaseStats) error {
	parts := []string{}
	args := []any{}
	if hasIndexes {
		q := fmt.Sprintf(`SELECT database_name,
  SUM(total_indexes) AS total_indexes,
  SUM(done_indexes) AS done_indexes,
  0 AS total_statistics, 0 AS done_statistics,
  SUM(total_rows) AS total_rows,
  SUM(total_pages) AS total_pages,
  MAX(max_fragmentation) AS max_fragmentation,
  MAX(last_scan) AS last_scan,
  MAX(last_defrag) AS last_defrag,
  CAST(NULL AS DATETIME) AS last_stats_update
FROM (
  SELECT dbName AS database_name,
    objectID,
    COUNT(1) AS total_indexes,
    COALESCE(SUM(CASE WHEN defragDate IS NOT NULL OR printStatus = 1 THEN 1 ELSE 0 END), 0) AS done_indexes,
    COALESCE(NULLIF(SUM(CASE WHEN indexID IN (0, 1) THEN CAST(record_count AS bigint) ELSE 0 END), 0), MAX(CAST(record_count AS bigint)), 0) AS total_rows,
    COALESCE(SUM(CAST(page_count AS bigint)), 0) AS total_pages,
    MAX(fragmentation) AS max_fragmentation,
    MAX(scanDate) AS last_scan,
    MAX(defragDate) AS last_defrag
  FROM %s`, qualifyName(maintenanceDB, "dbo", "tbl_AdaptiveIndexDefrag_Working"))
		q, args = appendDatabaseWhere(q, args, database)
		q += ` GROUP BY dbName, objectID
) i
GROUP BY database_name`
		parts = append(parts, q)
	}
	if hasStats {
		q := fmt.Sprintf(`SELECT dbName AS database_name, 0 AS total_indexes, 0 AS done_indexes,
  COUNT(1) AS total_statistics,
  COALESCE(SUM(CASE WHEN updateDate IS NOT NULL OR printStatus = 1 THEN 1 ELSE 0 END), 0) AS done_statistics,
  0 AS total_rows,
  0 AS total_pages,
  CAST(NULL AS float) AS max_fragmentation,
  MAX(scanDate) AS last_scan, CAST(NULL AS DATETIME) AS last_defrag, MAX(updateDate) AS last_stats_update
FROM %s`, qualifyName(maintenanceDB, "dbo", "tbl_AdaptiveIndexDefrag_Stats_Working"))
		q, args = appendDatabaseWhere(q, args, database)
		q += ` GROUP BY dbName`
		parts = append(parts, q)
	}
	query := `
SELECT database_name,
  SUM(total_indexes) AS total_indexes,
  SUM(done_indexes) AS done_indexes,
  SUM(total_statistics) AS total_statistics,
  SUM(done_statistics) AS done_statistics,
  SUM(total_rows) AS total_rows,
  SUM(total_pages) AS total_pages,
  MAX(max_fragmentation) AS max_fragmentation,
  MAX(last_scan) AS last_scan,
  MAX(last_defrag) AS last_defrag,
  MAX(last_stats_update) AS last_stats_update
FROM (` + strings.Join(parts, "\nUNION ALL\n") + `) x
GROUP BY database_name
ORDER BY database_name`
	if err := db.WithContext(ctx).Raw(query, args...).Scan(out).Error; err != nil {
		return fmt.Errorf("query defrag database stats: %w", err)
	}
	return nil
}

func scanTableStats(ctx context.Context, db *gorm.DB, maintenanceDB, database, table string, hasIndexes, hasStats bool, out *[]TableStats) error {
	parts := []string{}
	args := []any{}
	if hasIndexes {
		q := fmt.Sprintf(`SELECT dbName AS database_name, COALESCE(schemaName, '') AS schema_name, objectName AS object_name,
  CASE WHEN schemaName IS NULL OR schemaName = '' THEN objectName ELSE schemaName + '.' + objectName END AS table_name,
  COUNT(1) AS total_indexes,
  COALESCE(SUM(CASE WHEN defragDate IS NOT NULL OR printStatus = 1 THEN 1 ELSE 0 END), 0) AS done_indexes,
  0 AS total_statistics, 0 AS done_statistics,
  COALESCE(NULLIF(SUM(CASE WHEN indexID IN (0, 1) THEN CAST(record_count AS bigint) ELSE 0 END), 0), MAX(CAST(record_count AS bigint)), 0) AS total_rows,
  MAX(fragmentation) AS max_fragmentation,
  COALESCE(SUM(CAST(page_count AS bigint)), 0) AS total_pages,
  MAX(scanDate) AS last_scan, MAX(defragDate) AS last_defrag, CAST(NULL AS DATETIME) AS last_stats_update
FROM %s`, qualifyName(maintenanceDB, "dbo", "tbl_AdaptiveIndexDefrag_Working"))
		q, args = appendDatabaseTableWhere(q, args, database, table)
		q += ` GROUP BY dbName, schemaName, objectName`
		parts = append(parts, q)
	}
	if hasStats {
		q := fmt.Sprintf(`SELECT dbName AS database_name, COALESCE(schemaName, '') AS schema_name, objectName AS object_name,
  CASE WHEN schemaName IS NULL OR schemaName = '' THEN objectName ELSE schemaName + '.' + objectName END AS table_name,
  0 AS total_indexes, 0 AS done_indexes,
  COUNT(1) AS total_statistics,
  COALESCE(SUM(CASE WHEN updateDate IS NOT NULL OR printStatus = 1 THEN 1 ELSE 0 END), 0) AS done_statistics,
  0 AS total_rows,
  CAST(NULL AS float) AS max_fragmentation,
  0 AS total_pages,
  MAX(scanDate) AS last_scan, CAST(NULL AS DATETIME) AS last_defrag, MAX(updateDate) AS last_stats_update
FROM %s`, qualifyName(maintenanceDB, "dbo", "tbl_AdaptiveIndexDefrag_Stats_Working"))
		q, args = appendDatabaseTableWhere(q, args, database, table)
		q += ` GROUP BY dbName, schemaName, objectName`
		parts = append(parts, q)
	}
	query := `
SELECT database_name, schema_name, object_name, table_name,
  SUM(total_indexes) AS total_indexes,
  SUM(done_indexes) AS done_indexes,
  SUM(total_statistics) AS total_statistics,
  SUM(done_statistics) AS done_statistics,
  SUM(total_rows) AS total_rows,
  MAX(max_fragmentation) AS max_fragmentation,
  SUM(total_pages) AS total_pages,
  MAX(last_scan) AS last_scan,
  MAX(last_defrag) AS last_defrag,
  MAX(last_stats_update) AS last_stats_update
FROM (` + strings.Join(parts, "\nUNION ALL\n") + `) x
GROUP BY database_name, schema_name, object_name, table_name
ORDER BY database_name, table_name`
	if err := db.WithContext(ctx).Raw(query, args...).Scan(out).Error; err != nil {
		return fmt.Errorf("query defrag table stats: %w", err)
	}
	return nil
}

func scanIndexStats(ctx context.Context, db *gorm.DB, maintenanceDB, database, table string, hasIndexes, hasStats bool, out *[]IndexStats) error {
	parts := []string{}
	args := []any{}
	if hasIndexes {
		q := fmt.Sprintf(`SELECT dbName AS database_name, COALESCE(schemaName, '') AS schema_name, objectName AS object_name,
  CASE WHEN schemaName IS NULL OR schemaName = '' THEN objectName ELSE schemaName + '.' + objectName END AS table_name,
  COALESCE(indexName, '') AS name,
  'index' AS kind,
  partitionNumber AS partition_number,
  CAST(CASE WHEN defragDate IS NOT NULL OR printStatus = 1 THEN 1 ELSE 0 END AS bit) AS done,
  fragmentation,
  CAST(page_count AS bigint) AS page_count,
  CAST(record_count AS bigint) AS record_count,
  range_scan_count,
  scanDate AS scan_date,
  defragDate AS completed_at
FROM %s`, qualifyName(maintenanceDB, "dbo", "tbl_AdaptiveIndexDefrag_Working"))
		q, args = appendDatabaseTableWhere(q, args, database, table)
		parts = append(parts, q)
	}
	if hasStats {
		q := fmt.Sprintf(`SELECT dbName AS database_name, COALESCE(schemaName, '') AS schema_name, objectName AS object_name,
  CASE WHEN schemaName IS NULL OR schemaName = '' THEN objectName ELSE schemaName + '.' + objectName END AS table_name,
  COALESCE(statsName, '') AS name,
  'statistic' AS kind,
  partitionNumber AS partition_number,
  CAST(CASE WHEN updateDate IS NOT NULL OR printStatus = 1 THEN 1 ELSE 0 END AS bit) AS done,
  CAST(NULL AS float) AS fragmentation,
  CAST(NULL AS bigint) AS page_count,
  CAST(NULL AS bigint) AS record_count,
  CAST(NULL AS bigint) AS range_scan_count,
  scanDate AS scan_date,
  updateDate AS completed_at
FROM %s`, qualifyName(maintenanceDB, "dbo", "tbl_AdaptiveIndexDefrag_Stats_Working"))
		q, args = appendDatabaseTableWhere(q, args, database, table)
		parts = append(parts, q)
	}
	query := strings.Join(parts, "\nUNION ALL\n") + `
ORDER BY database_name, table_name, kind, name, partition_number`
	if err := db.WithContext(ctx).Raw(query, args...).Scan(out).Error; err != nil {
		return fmt.Errorf("query defrag index stats: %w", err)
	}
	return nil
}

func appendDatabaseWhere(query string, args []any, database string) (string, []any) {
	if database == "" {
		return query, args
	}
	query += ` WHERE dbName IN (?, ?)`
	return query, append(args, database, bracketName(database))
}

func appendDatabaseTableWhere(query string, args []any, database, table string) (string, []any) {
	var clauses []string
	if database != "" {
		clauses = append(clauses, `dbName IN (?, ?)`)
		args = append(args, database, bracketName(database))
	}
	table = strings.TrimSpace(table)
	if table != "" {
		clauses = append(clauses, `(objectName = ? OR COALESCE(schemaName, '') + '.' + objectName = ?)`)
		args = append(args, table, table)
	}
	if len(clauses) == 0 {
		return query, args
	}
	return query + ` WHERE ` + strings.Join(clauses, ` AND `), args
}
