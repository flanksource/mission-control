package sqldefrag

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/golang-sql/sqlexp"
	"gorm.io/gorm"
)

const (
	DefaultScriptURL           = "https://raw.githubusercontent.com/microsoft/tigertoolbox/master/AdaptiveIndexDefrag/usp_AdaptiveIndexDefrag.sql"
	DefaultMaintenanceDatabase = "msdb"
	maxScriptBytes             = 5 << 20
)

type InstallOptions struct {
	Source              string `json:"source,omitempty"`
	MaintenanceDatabase string `json:"maintenanceDatabase,omitempty"`
}

type InstallResult struct {
	Source      string        `json:"source"`
	Database    string        `json:"database"`
	Batches     int           `json:"batches"`
	Installed   bool          `json:"installed"`
	Version     string        `json:"version,omitempty"`
	Duration    time.Duration `json:"duration"`
	InstalledAt time.Time     `json:"installedAt"`
}

type RunOptions struct {
	Database                    string  `json:"database,omitempty"`
	MaintenanceDatabase         string  `json:"maintenanceDatabase,omitempty"`
	Table                       string  `json:"table,omitempty"`
	Execute                     bool    `json:"execute"`
	PrintCommands               bool    `json:"printCommands"`
	OutputResults               bool    `json:"outputResults"`
	Debug                       bool    `json:"debug"`
	TimeLimitMinutes            int     `json:"timeLimitMinutes"`
	ForceRescan                 bool    `json:"forceRescan"`
	Delay                       string  `json:"delay"`
	OrderColumn                 string  `json:"orderColumn"`
	SortOrder                   string  `json:"sortOrder"`
	IndexType                   string  `json:"indexType,omitempty"`
	MinFragmentation            float64 `json:"minFragmentation"`
	RebuildThreshold            float64 `json:"rebuildThreshold"`
	RebuildThresholdColumnstore float64 `json:"rebuildThresholdColumnstore"`
	MinPageCount                int     `json:"minPageCount"`
	MaxPageCount                int     `json:"maxPageCount,omitempty"`
	FillFactor                  string  `json:"fillFactor,omitempty"`
	ScanMode                    string  `json:"scanMode"`
	OnlineRebuild               bool    `json:"onlineRebuild"`
	ResumableRebuild            bool    `json:"resumableRebuild"`
	SortInTempDB                bool    `json:"sortInTempDB"`
	MaxDopRestriction           int     `json:"maxDopRestriction,omitempty"`
	UpdateStats                 bool    `json:"updateStats"`
	UpdateStatsWhere            bool    `json:"updateStatsWhere"`
	StatsSample                 string  `json:"statsSample,omitempty"`
	PersistStatsSample          string  `json:"persistStatsSample,omitempty"`
	StatsThreshold              float64 `json:"statsThreshold,omitempty"`
	StatsMinRows                int64   `json:"statsMinRows,omitempty"`
	IxStatsNoRecompute          bool    `json:"ixStatsNoRecompute"`
	StatsIncremental            string  `json:"statsIncremental,omitempty"`
	PartitionMode               string  `json:"partitionMode,omitempty"`
	SkipLOBCompaction           bool    `json:"skipLobCompaction"`
	IgnoreDroppedObjects        bool    `json:"ignoreDroppedObjects"`
	DisableNCIX                 bool    `json:"disableNcix"`
	OfflineLockTimeout          int     `json:"offlineLockTimeout"`
	OnlineLockTimeout           int     `json:"onlineLockTimeout"`
	AbortAfterWait              string  `json:"abortAfterWait,omitempty"`
	CompressAllRowGroups        bool    `json:"compressAllRowGroups"`
	IncludeBlobFragmentation    bool    `json:"includeBlobFragmentation"`
	DataCompression             string  `json:"dataCompression,omitempty"`
	StopExisting                bool    `json:"stopExisting,omitempty"`
	TerminateExisting           bool    `json:"terminateExisting,omitempty"`
}

type RunResult struct {
	Database   string         `json:"database,omitempty"`
	Table      string         `json:"table,omitempty"`
	StartedAt  time.Time      `json:"startedAt"`
	FinishedAt time.Time      `json:"finishedAt"`
	Duration   time.Duration  `json:"duration"`
	SQL        string         `json:"sql"`
	Status     Status         `json:"status"`
	Messages   []RunMessage   `json:"messages,omitempty"`
	ResultSets []RunResultSet `json:"resultSets,omitempty"`
	Error      string         `json:"error,omitempty"`
}

type RunMessage struct {
	Type    string `json:"type"`
	Message string `json:"message,omitempty"`
	Count   *int64 `json:"count,omitempty"`
	Error   string `json:"error,omitempty"`
}

type RunResultSet struct {
	Columns   []string         `json:"columns"`
	Rows      []map[string]any `json:"rows"`
	RowCount  int              `json:"rowCount"`
	Truncated bool             `json:"truncated,omitempty"`
}

type Status struct {
	Installed           bool       `json:"installed"`
	Version             string     `json:"version,omitempty"`
	MaintenanceDatabase string     `json:"maintenanceDatabase"`
	ProcedureName       string     `json:"procedureName"`
	CreatedAt           *time.Time `json:"createdAt,omitempty"`
	ModifiedAt          *time.Time `json:"modifiedAt,omitempty"`
	Database            string     `json:"database,omitempty"`
	Running             bool       `json:"running"`
	TotalIndexes        int64      `json:"totalIndexes"`
	DoneIndexes         int64      `json:"doneIndexes"`
	TotalStatistics     int64      `json:"totalStatistics"`
	DoneStatistics      int64      `json:"doneStatistics"`
	HistoryRows         int64      `json:"historyRows"`
	Errors24h           int64      `json:"errors24h"`
	LastRunStart        *time.Time `json:"lastRunStart,omitempty"`
	LastRunEnd          *time.Time `json:"lastRunEnd,omitempty"`
	LastError           *time.Time `json:"lastError,omitempty"`
	LastErrorMessage    string     `json:"lastErrorMessage,omitempty"`
}

type HistoryOptions struct {
	Database            string `json:"database,omitempty"`
	MaintenanceDatabase string `json:"maintenanceDatabase,omitempty"`
	Limit               int    `json:"limit,omitempty"`
}

type StopOptions struct {
	MaintenanceDatabase string `json:"maintenanceDatabase,omitempty"`
}

type DefragSession struct {
	SessionID           int        `json:"sessionId" gorm:"column:session_id"`
	Status              string     `json:"status,omitempty" gorm:"column:status"`
	Database            string     `json:"database,omitempty" gorm:"column:database_name"`
	MaintenanceDatabase string     `json:"maintenanceDatabase,omitempty"`
	Command             string     `json:"command,omitempty" gorm:"column:command"`
	StartedAt           *time.Time `json:"startedAt,omitempty" gorm:"column:started_at"`
	SQL                 string     `json:"sql,omitempty" gorm:"column:sql_text"`
}

type TerminateResult struct {
	MaintenanceDatabase string          `json:"maintenanceDatabase"`
	Terminated          []DefragSession `json:"terminated,omitempty"`
	Errors              []RunMessage    `json:"errors,omitempty"`
}

type HistoryRow struct {
	Database        string     `json:"database" gorm:"column:database_name"`
	ObjectName      string     `json:"objectName,omitempty" gorm:"column:object_name"`
	IndexName       string     `json:"indexName,omitempty" gorm:"column:index_name"`
	StatsName       string     `json:"statsName,omitempty" gorm:"column:stats_name"`
	PartitionNumber *int       `json:"partitionNumber,omitempty" gorm:"column:partition_number"`
	Fragmentation   *float64   `json:"fragmentation,omitempty" gorm:"column:fragmentation"`
	PageCount       *int64     `json:"pageCount,omitempty" gorm:"column:page_count"`
	RangeScanCount  *int64     `json:"rangeScanCount,omitempty" gorm:"column:range_scan_count"`
	StartedAt       time.Time  `json:"startedAt" gorm:"column:started_at"`
	FinishedAt      *time.Time `json:"finishedAt,omitempty" gorm:"column:finished_at"`
	DurationSeconds *int       `json:"durationSeconds,omitempty" gorm:"column:duration_seconds"`
	Operation       string     `json:"operation" gorm:"column:operation"`
	SQL             string     `json:"sql,omitempty" gorm:"column:sql_statement"`
	Error           string     `json:"error,omitempty" gorm:"column:error_message"`
}

func Install(ctx context.Context, db *gorm.DB, opts InstallOptions) (InstallResult, error) {
	source := normalizeSource(opts.Source)
	maintenanceDB := normalizeMaintenanceDatabase(opts.MaintenanceDatabase)
	started := time.Now()
	script, err := loadScript(ctx, source)
	if err != nil {
		return InstallResult{}, err
	}
	script = prepareInstallScript(script, maintenanceDB)
	batches := SplitSQLBatches(script)
	if len(batches) == 0 {
		return InstallResult{}, fmt.Errorf("script contains no executable batches")
	}
	if err := executeBatches(ctx, db, maintenanceDB, batches); err != nil {
		return InstallResult{}, err
	}
	status, err := GetStatus(ctx, db, StatusOptions{MaintenanceDatabase: maintenanceDB})
	if err != nil {
		return InstallResult{}, err
	}
	return InstallResult{
		Source:      source,
		Database:    maintenanceDB,
		Batches:     len(batches),
		Installed:   status.Installed,
		Version:     status.Version,
		Duration:    time.Since(started),
		InstalledAt: time.Now(),
	}, nil
}

func Run(ctx context.Context, db *gorm.DB, opts RunOptions) (RunResult, error) {
	normalized, err := normalizeRunOptions(ctx, db, opts)
	if err != nil {
		return RunResult{}, err
	}
	if normalized.StopExisting || normalized.TerminateExisting {
		if _, err := TerminateExistingRuns(ctx, db, StopOptions{MaintenanceDatabase: normalized.MaintenanceDatabase}); err != nil {
			return RunResult{}, err
		}
	}
	sql, args, err := BuildRunSQL(normalized)
	if err != nil {
		return RunResult{}, err
	}
	started := time.Now()
	result := RunResult{
		Database:  normalized.Database,
		Table:     normalized.Table,
		StartedAt: started,
		SQL:       sql,
	}
	messages, resultSets, execErr := executeRun(ctx, db, normalized.MaintenanceDatabase, sql, args)
	finished := time.Now()
	result.FinishedAt = finished
	result.Duration = finished.Sub(started)
	result.Messages = messages
	result.ResultSets = resultSets
	status, err := GetStatus(ctx, db, StatusOptions{Database: normalized.Database, MaintenanceDatabase: normalized.MaintenanceDatabase})
	if err != nil {
		if execErr == nil {
			execErr = err
		} else {
			messages = append(messages, RunMessage{Type: "error", Error: err.Error()})
			result.Messages = messages
		}
	}
	result.Status = status
	if execErr != nil {
		result.Error = execErr.Error()
		return result, execErr
	}
	return result, nil
}

func executeRun(ctx context.Context, db *gorm.DB, maintenanceDB, query string, args []any) ([]RunMessage, []RunResultSet, error) {
	sqlDB, err := db.DB()
	if err != nil {
		return nil, nil, fmt.Errorf("get sql db: %w", err)
	}
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("open sql connection: %w", err)
	}
	defer conn.Close()

	maintenanceDB = normalizeMaintenanceDatabase(maintenanceDB)
	if _, err := conn.ExecContext(ctx, "USE "+bracketName(maintenanceDB)); err != nil {
		return nil, nil, fmt.Errorf("use %s: %w", maintenanceDB, err)
	}

	query, args = sqlServerQueryArgs(query, args)
	retmsg := &sqlexp.ReturnMessage{}
	queryArgs := make([]any, 0, len(args)+1)
	queryArgs = append(queryArgs, retmsg)
	queryArgs = append(queryArgs, args...)
	rows, err := conn.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return nil, nil, fmt.Errorf("execute usp_AdaptiveIndexDefrag: %w", err)
	}
	defer rows.Close()

	var messages []RunMessage
	var resultSets []RunResultSet
	var execErr error
	active := true
	for active {
		msg := retmsg.Message(ctx)
		switch m := msg.(type) {
		case sqlexp.MsgNotice:
			messages = append(messages, RunMessage{Type: "notice", Message: m.Message.String()})
		case sqlexp.MsgRowsAffected:
			count := m.Count
			messages = append(messages, RunMessage{Type: "rowsAffected", Count: &count})
		case sqlexp.MsgError:
			messages = append(messages, RunMessage{Type: "error", Error: m.Error.Error()})
			if execErr == nil {
				execErr = m.Error
			}
		case sqlexp.MsgNext:
			resultSet, err := collectResultSet(rows, 200)
			if err != nil {
				messages = append(messages, RunMessage{Type: "error", Error: err.Error()})
				if execErr == nil {
					execErr = err
				}
				continue
			}
			resultSets = append(resultSets, resultSet)
		case sqlexp.MsgNextResultSet:
			active = rows.NextResultSet()
		default:
			messages = append(messages, RunMessage{Type: "message", Message: fmt.Sprint(m)})
		}
	}
	if err := rows.Err(); err != nil {
		messages = append(messages, RunMessage{Type: "error", Error: err.Error()})
		if execErr == nil {
			execErr = err
		}
	}
	if execErr != nil {
		return messages, resultSets, fmt.Errorf("execute usp_AdaptiveIndexDefrag: %w", execErr)
	}
	return messages, resultSets, nil
}

func collectResultSet(rows *sql.Rows, limit int) (RunResultSet, error) {
	columns, err := rows.Columns()
	if err != nil {
		return RunResultSet{}, fmt.Errorf("read result columns: %w", err)
	}
	out := RunResultSet{Columns: columns}
	for rows.Next() {
		values := make([]any, len(columns))
		scan := make([]any, len(columns))
		for i := range values {
			scan[i] = &values[i]
		}
		if err := rows.Scan(scan...); err != nil {
			return out, fmt.Errorf("scan result row: %w", err)
		}
		out.RowCount++
		if len(out.Rows) >= limit {
			out.Truncated = true
			continue
		}
		row := make(map[string]any, len(columns))
		for i, column := range columns {
			row[column] = normalizeResultValue(values[i])
		}
		out.Rows = append(out.Rows, row)
	}
	return out, nil
}

func normalizeResultValue(value any) any {
	switch v := value.(type) {
	case nil:
		return nil
	case []byte:
		return string(v)
	case time.Time:
		return v
	default:
		return v
	}
}

func sqlServerQueryArgs(query string, args []any) (string, []any) {
	out := query
	named := make([]any, 0, len(args))
	for i, arg := range args {
		name := fmt.Sprintf("p%d", i+1)
		out = strings.Replace(out, "?", "@"+name, 1)
		named = append(named, sql.Named(name, arg))
	}
	return out, named
}

type StatusOptions struct {
	Database            string `json:"database,omitempty"`
	MaintenanceDatabase string `json:"maintenanceDatabase,omitempty"`
}

func GetStatus(ctx context.Context, db *gorm.DB, opts StatusOptions) (Status, error) {
	database, err := resolveDatabase(ctx, db, opts.Database)
	if err != nil {
		return Status{}, err
	}
	maintenanceDB := normalizeMaintenanceDatabase(opts.MaintenanceDatabase)
	out := Status{
		MaintenanceDatabase: maintenanceDB,
		ProcedureName:       qualifyName(maintenanceDB, "dbo", "usp_AdaptiveIndexDefrag"),
		Database:            database,
	}

	var count int64
	if err := db.WithContext(ctx).Raw(fmt.Sprintf(`
SELECT COUNT(1)
FROM %s p
JOIN %s s ON s.schema_id = p.schema_id
WHERE s.name = N'dbo' AND p.name = N'usp_AdaptiveIndexDefrag'`,
		qualifyName(maintenanceDB, "sys", "procedures"),
		qualifyName(maintenanceDB, "sys", "schemas"),
	)).Scan(&count).Error; err != nil {
		return out, fmt.Errorf("check AdaptiveIndexDefrag install: %w", err)
	}
	out.Installed = count > 0
	if !out.Installed {
		return out, nil
	}

	var version struct {
		Version    string     `gorm:"column:version"`
		CreatedAt  *time.Time `gorm:"column:created_at"`
		ModifiedAt *time.Time `gorm:"column:modified_at"`
	}
	if err := db.WithContext(ctx).Raw(fmt.Sprintf(`
SELECT TOP (1)
  CASE
    WHEN CHARINDEX('PROCEDURE @VER: ', m.definition) > 0
      THEN 'AdaptiveIndexDefrag ' + SUBSTRING(m.definition, CHARINDEX('PROCEDURE @VER: ', m.definition) + 16, 5)
    ELSE 'AdaptiveIndexDefrag'
  END AS version,
  o.create_date AS created_at,
  o.modify_date AS modified_at
FROM %s m
JOIN %s o ON o.object_id = m.object_id
JOIN %s s ON s.schema_id = o.schema_id
WHERE s.name = N'dbo' AND o.name = N'usp_AdaptiveIndexDefrag'`,
		qualifyName(maintenanceDB, "sys", "sql_modules"),
		qualifyName(maintenanceDB, "sys", "objects"),
		qualifyName(maintenanceDB, "sys", "schemas"),
	)).Scan(&version).Error; err != nil {
		return out, fmt.Errorf("query AdaptiveIndexDefrag version: %w", err)
	}
	out.Version = version.Version
	out.CreatedAt = version.CreatedAt
	out.ModifiedAt = version.ModifiedAt

	out.Running = currentRequestCount(ctx, db, maintenanceDB) > 0

	if tableExists(ctx, db, maintenanceDB, "tbl_AdaptiveIndexDefrag_Working") {
		var p struct {
			Total int64 `gorm:"column:total"`
			Done  int64 `gorm:"column:done"`
		}
		query := `SELECT COUNT(1) AS total, COALESCE(SUM(CASE WHEN defragDate IS NOT NULL OR printStatus = 1 THEN 1 ELSE 0 END), 0) AS done FROM ` + qualifyName(maintenanceDB, "dbo", "tbl_AdaptiveIndexDefrag_Working")
		args := []any{}
		if database != "" {
			query += ` WHERE dbName IN (?, ?)`
			args = append(args, database, bracketName(database))
		}
		if err := db.WithContext(ctx).Raw(query, args...).Scan(&p).Error; err != nil {
			return out, fmt.Errorf("query defrag index progress: %w", err)
		}
		out.TotalIndexes = p.Total
		out.DoneIndexes = p.Done
	}

	if tableExists(ctx, db, maintenanceDB, "tbl_AdaptiveIndexDefrag_Stats_Working") {
		var p struct {
			Total int64 `gorm:"column:total"`
			Done  int64 `gorm:"column:done"`
		}
		query := `SELECT COUNT(1) AS total, COALESCE(SUM(CASE WHEN updateDate IS NOT NULL OR printStatus = 1 THEN 1 ELSE 0 END), 0) AS done FROM ` + qualifyName(maintenanceDB, "dbo", "tbl_AdaptiveIndexDefrag_Stats_Working")
		args := []any{}
		if database != "" {
			query += ` WHERE dbName IN (?, ?)`
			args = append(args, database, bracketName(database))
		}
		if err := db.WithContext(ctx).Raw(query, args...).Scan(&p).Error; err != nil {
			return out, fmt.Errorf("query defrag statistics progress: %w", err)
		}
		out.TotalStatistics = p.Total
		out.DoneStatistics = p.Done
	}

	if tableExists(ctx, db, maintenanceDB, "tbl_AdaptiveIndexDefrag_log") || tableExists(ctx, db, maintenanceDB, "tbl_AdaptiveIndexDefrag_Stats_log") {
		if err := scanLogSummary(ctx, db, maintenanceDB, database, &out); err != nil {
			return out, err
		}
	}
	return out, nil
}

func History(ctx context.Context, db *gorm.DB, opts HistoryOptions) ([]HistoryRow, error) {
	database, err := resolveDatabase(ctx, db, opts.Database)
	if err != nil {
		return nil, err
	}
	maintenanceDB := normalizeMaintenanceDatabase(opts.MaintenanceDatabase)
	hasIndexLog := tableExists(ctx, db, maintenanceDB, "tbl_AdaptiveIndexDefrag_log")
	hasStatsLog := tableExists(ctx, db, maintenanceDB, "tbl_AdaptiveIndexDefrag_Stats_log")
	if !hasIndexLog && !hasStatsLog {
		return []HistoryRow{}, nil
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}

	parts := []string{}
	if hasIndexLog {
		parts = append(parts, fmt.Sprintf(`
  SELECT
    dbName AS database_name,
    objectName AS object_name,
    indexName AS index_name,
    CAST(NULL AS NVARCHAR(256)) AS stats_name,
    partitionNumber AS partition_number,
    fragmentation,
    page_count,
    range_scan_count,
    dateTimeStart AS started_at,
    dateTimeEnd AS finished_at,
    durationSeconds AS duration_seconds,
    CASE
      WHEN sqlStatement LIKE '%%REORGANIZE%%' THEN 'Reorg'
      WHEN sqlStatement LIKE '%%REBUILD%%' THEN 'Rebuild'
      ELSE 'Index'
    END AS operation,
    sqlStatement AS sql_statement,
    errorMessage AS error_message
  FROM %s`, qualifyName(maintenanceDB, "dbo", "tbl_AdaptiveIndexDefrag_log")))
	}
	if hasStatsLog {
		parts = append(parts, fmt.Sprintf(`
  SELECT
    dbName AS database_name,
    objectName AS object_name,
    CAST(NULL AS NVARCHAR(256)) AS index_name,
    statsName AS stats_name,
    partitionNumber AS partition_number,
    CAST(NULL AS float) AS fragmentation,
    CAST(NULL AS bigint) AS page_count,
    CAST(NULL AS bigint) AS range_scan_count,
    dateTimeStart AS started_at,
    dateTimeEnd AS finished_at,
    durationSeconds AS duration_seconds,
    'UpdateStats' AS operation,
    sqlStatement AS sql_statement,
    errorMessage AS error_message
  FROM %s`, qualifyName(maintenanceDB, "dbo", "tbl_AdaptiveIndexDefrag_Stats_log")))
	}
	query := fmt.Sprintf(`
SELECT TOP (%d)
  database_name,
  object_name,
  index_name,
  stats_name,
  partition_number,
  fragmentation,
  page_count,
  range_scan_count,
  started_at,
  finished_at,
  duration_seconds,
  operation,
  sql_statement,
  error_message
FROM (`+strings.Join(parts, "\nUNION ALL\n")+`) h`, limit)
	args := []any{}
	if database != "" {
		query += ` WHERE database_name IN (?, ?)`
		args = append(args, database, bracketName(database))
	}
	query += ` ORDER BY started_at DESC`

	var rows []HistoryRow
	if err := db.WithContext(ctx).Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("query defrag history: %w", err)
	}
	return rows, nil
}

func BuildRunSQL(opts RunOptions) (string, []any, error) {
	if opts.TimeLimitMinutes <= 0 {
		opts.TimeLimitMinutes = 60
	}
	if opts.Delay == "" {
		opts.Delay = "00:00:05"
	}
	if opts.OrderColumn == "" {
		opts.OrderColumn = "range_scan_count"
	}
	if opts.SortOrder == "" {
		opts.SortOrder = "DESC"
	}
	if opts.MinFragmentation == 0 {
		opts.MinFragmentation = 5
	}
	if opts.RebuildThreshold == 0 {
		opts.RebuildThreshold = 30
	}
	if opts.RebuildThresholdColumnstore == 0 {
		opts.RebuildThresholdColumnstore = 10
	}
	if opts.MinPageCount == 0 {
		opts.MinPageCount = 8
	}
	if opts.ScanMode == "" {
		opts.ScanMode = "LIMITED"
	}
	if opts.OfflineLockTimeout == 0 {
		opts.OfflineLockTimeout = -1
	}
	if opts.OnlineLockTimeout == 0 {
		opts.OnlineLockTimeout = 5
	}
	order := strings.ToLower(strings.TrimSpace(opts.OrderColumn))
	switch order {
	case "range_scan_count", "fragmentation", "page_count":
	default:
		return "", nil, fmt.Errorf("orderColumn must be range_scan_count, fragmentation, or page_count")
	}
	sortOrder := strings.ToUpper(strings.TrimSpace(opts.SortOrder))
	if sortOrder != "ASC" && sortOrder != "DESC" {
		return "", nil, fmt.Errorf("sortOrder must be ASC or DESC")
	}
	indexType, err := indexTypeOption(opts.IndexType)
	if err != nil {
		return "", nil, err
	}
	scanMode := strings.ToUpper(strings.TrimSpace(opts.ScanMode))
	switch scanMode {
	case "LIMITED", "SAMPLED", "DETAILED":
	default:
		return "", nil, fmt.Errorf("scanMode must be LIMITED, SAMPLED, or DETAILED")
	}
	persistStatsSample, err := tristateOption("persistStatsSample", opts.PersistStatsSample)
	if err != nil {
		return "", nil, err
	}
	statsIncremental, err := tristateOption("statsIncremental", opts.StatsIncremental)
	if err != nil {
		return "", nil, err
	}
	partitionMode, err := partitionModeOption(opts.PartitionMode)
	if err != nil {
		return "", nil, err
	}
	abortAfterWait, err := abortAfterWaitOption(opts.AbortAfterWait)
	if err != nil {
		return "", nil, err
	}
	statsSample, err := statsSampleOption(opts.StatsSample)
	if err != nil {
		return "", nil, err
	}
	dataCompression, err := dataCompressionOption(opts.DataCompression)
	if err != nil {
		return "", nil, err
	}
	fillFactor, err := fillFactorOption(opts.FillFactor)
	if err != nil {
		return "", nil, err
	}
	if opts.MaxPageCount < 0 {
		return "", nil, fmt.Errorf("maxPageCount must be >= 0")
	}
	if opts.MaxDopRestriction < 0 || opts.MaxDopRestriction > 8 {
		return "", nil, fmt.Errorf("maxDopRestriction must be between 0 and 8")
	}
	if opts.StatsThreshold < 0 || opts.StatsThreshold >= 100 {
		return "", nil, fmt.Errorf("statsThreshold must be 0 or between 0.001 and less than 100")
	}
	if opts.StatsThreshold > 0 && opts.StatsThreshold < 0.001 {
		return "", nil, fmt.Errorf("statsThreshold must be 0 or between 0.001 and less than 100")
	}
	if opts.StatsMinRows < 0 {
		return "", nil, fmt.Errorf("statsMinRows must be >= 0")
	}
	if !regexp.MustCompile(`^\d\d:[0-5]\d:[0-5]\d$`).MatchString(opts.Delay) {
		return "", nil, fmt.Errorf("delay must be HH:MM:SS")
	}

	sql := `EXEC dbo.usp_AdaptiveIndexDefrag
  @Exec_Print = ?,
  @printCmds = ?,
  @outputResults = ?,
  @debugMode = ?,
  @timeLimit = ?,
  @dbScope = ?,
  @tblName = ?,
  @defragOrderColumn = ?,
  @defragSortOrder = ?,
  @forceRescan = ?,
  @defragDelay = ?,
  @ixtypeOption = ?,
  @minFragmentation = ?,
  @rebuildThreshold = ?,
  @rebuildThreshold_cs = ?,
  @minPageCount = ?,
  @maxPageCount = ?,
  @fillfactor = ?,
  @scanMode = ?,
  @onlineRebuild = ?,
  @resumableRebuild = ?,
  @sortInTempDB = ?,
  @maxDopRestriction = ?,
  @updateStats = ?,
  @updateStatsWhere = ?,
  @statsSample = ?,
  @persistStatsSample = ?,
  @statsThreshold = ?,
  @statsMinRows = ?,
  @ix_statsnorecompute = ?,
  @statsIncremental = ?,
  @dealMaxPartition = ?,
  @dealLOB = ?,
  @ignoreDropObj = ?,
  @disableNCIX = ?,
  @offlinelocktimeout = ?,
  @onlinelocktimeout = ?,
  @abortAfterwait = ?,
  @dealROWG = ?,
  @getBlobfrag = ?,
  @dataCompression = ?`
	args := []any{
		bit(opts.Execute),
		bit(opts.PrintCommands),
		bit(opts.OutputResults),
		bit(opts.Debug),
		opts.TimeLimitMinutes,
		nullableString(opts.Database),
		nullableString(opts.Table),
		order,
		sortOrder,
		bit(opts.ForceRescan),
		opts.Delay,
		indexType,
		opts.MinFragmentation,
		opts.RebuildThreshold,
		opts.RebuildThresholdColumnstore,
		opts.MinPageCount,
		nullablePositiveInt(opts.MaxPageCount),
		fillFactor,
		scanMode,
		bit(opts.OnlineRebuild),
		bit(opts.ResumableRebuild),
		bit(opts.SortInTempDB),
		nullablePositiveInt(opts.MaxDopRestriction),
		bit(opts.UpdateStats),
		bit(opts.UpdateStatsWhere),
		statsSample,
		persistStatsSample,
		nullablePositiveFloat(opts.StatsThreshold),
		nullablePositiveInt64(opts.StatsMinRows),
		bit(opts.IxStatsNoRecompute),
		statsIncremental,
		partitionMode,
		bit(opts.SkipLOBCompaction),
		bit(opts.IgnoreDroppedObjects),
		bit(opts.DisableNCIX),
		opts.OfflineLockTimeout,
		opts.OnlineLockTimeout,
		abortAfterWait,
		bit(opts.CompressAllRowGroups),
		bit(opts.IncludeBlobFragmentation),
		dataCompression,
	}
	return sql, args, nil
}

func normalizeRunOptions(ctx context.Context, db *gorm.DB, opts RunOptions) (RunOptions, error) {
	database, err := resolveDatabase(ctx, db, opts.Database)
	if err != nil {
		return opts, err
	}
	opts.Database = database
	opts.MaintenanceDatabase = normalizeMaintenanceDatabase(opts.MaintenanceDatabase)
	return opts, nil
}

func resolveDatabase(ctx context.Context, db *gorm.DB, requested string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(requested)) {
	case "all", "*":
		return "", nil
	case "":
		var name string
		if err := db.WithContext(ctx).Raw("SELECT DB_NAME()").Scan(&name).Error; err != nil {
			return "", fmt.Errorf("resolve current database: %w", err)
		}
		return name, nil
	default:
		return strings.TrimSpace(requested), nil
	}
}

func executeBatches(ctx context.Context, db *gorm.DB, maintenanceDB string, batches []string) error {
	maintenanceDB = normalizeMaintenanceDatabase(maintenanceDB)
	return db.WithContext(ctx).Connection(func(tx *gorm.DB) error {
		if err := tx.Exec("USE " + bracketName(maintenanceDB)).Error; err != nil {
			return fmt.Errorf("use %s: %w", maintenanceDB, err)
		}
		for i, batch := range batches {
			if err := tx.Exec(batch).Error; err != nil {
				return fmt.Errorf("execute install batch %d/%d: %w", i+1, len(batches), err)
			}
		}
		return nil
	})
}

func SplitSQLBatches(sqlContent string) []string {
	goRegex := regexp.MustCompile(`(?im)^\s*GO\s*$`)
	parts := goRegex.Split(sqlContent, -1)
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		hasStatement := false
		for _, line := range strings.Split(trimmed, "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "--") {
				hasStatement = true
				break
			}
		}
		if hasStatement {
			out = append(out, trimmed)
		}
	}
	return out
}

func loadScript(ctx context.Context, source string) (string, error) {
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
		if err != nil {
			return "", err
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", fmt.Errorf("download %s: %w", source, err)
		}
		defer res.Body.Close()
		if res.StatusCode < 200 || res.StatusCode >= 300 {
			return "", fmt.Errorf("download %s: HTTP %d", source, res.StatusCode)
		}
		body, err := io.ReadAll(io.LimitReader(res.Body, maxScriptBytes+1))
		if err != nil {
			return "", fmt.Errorf("read %s: %w", source, err)
		}
		if len(body) > maxScriptBytes {
			return "", fmt.Errorf("script exceeds %d bytes", maxScriptBytes)
		}
		return string(body), nil
	}
	body, err := os.ReadFile(source)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", source, err)
	}
	return string(body), nil
}

func normalizeSource(source string) string {
	source = strings.TrimSpace(source)
	if source == "" {
		return DefaultScriptURL
	}
	const blobPrefix = "https://github.com/microsoft/tigertoolbox/blob/master/"
	if strings.HasPrefix(source, blobPrefix) {
		return "https://raw.githubusercontent.com/microsoft/tigertoolbox/master/" + strings.TrimPrefix(source, blobPrefix)
	}
	return source
}

func prepareInstallScript(script, maintenanceDB string) string {
	maintenanceDB = normalizeMaintenanceDatabase(maintenanceDB)
	if strings.EqualFold(maintenanceDB, DefaultMaintenanceDatabase) {
		return script
	}
	useMSDB := regexp.MustCompile(`(?im)^\s*USE\s+\[msdb\]\s*;?\s*$`)
	return useMSDB.ReplaceAllString(script, "USE "+bracketName(maintenanceDB))
}

func scanLogSummary(ctx context.Context, db *gorm.DB, maintenanceDB, database string, out *Status) error {
	maintenanceDB = normalizeMaintenanceDatabase(maintenanceDB)
	parts := []string{}
	if tableExists(ctx, db, maintenanceDB, "tbl_AdaptiveIndexDefrag_log") {
		parts = append(parts, fmt.Sprintf(`
SELECT dbName, dateTimeStart, dateTimeEnd, errorMessage
FROM %s`, qualifyName(maintenanceDB, "dbo", "tbl_AdaptiveIndexDefrag_log")))
	}
	if tableExists(ctx, db, maintenanceDB, "tbl_AdaptiveIndexDefrag_Stats_log") {
		parts = append(parts, fmt.Sprintf(`
SELECT dbName, dateTimeStart, dateTimeEnd, errorMessage
FROM %s`, qualifyName(maintenanceDB, "dbo", "tbl_AdaptiveIndexDefrag_Stats_log")))
	}
	if len(parts) == 0 {
		return nil
	}
	query := `
SELECT
  COUNT(1) AS history_rows,
  COALESCE(SUM(CASE WHEN errorMessage IS NOT NULL AND dateTimeStart >= DATEADD(hour, -24, GETDATE()) THEN 1 ELSE 0 END), 0) AS errors_24h,
  MAX(dateTimeStart) AS last_run_start,
  MAX(dateTimeEnd) AS last_run_end,
  MAX(CASE WHEN errorMessage IS NOT NULL THEN dateTimeStart ELSE NULL END) AS last_error,
  (
    SELECT TOP (1) errorMessage
    FROM (` + strings.Join(parts, "\nUNION ALL\n") + `) e
    WHERE errorMessage IS NOT NULL`
	args := []any{}
	if database != "" {
		query += ` AND dbName IN (?, ?)`
		args = append(args, database, bracketName(database))
	}
	query += `
    ORDER BY dateTimeStart DESC
  ) AS last_error_message
FROM (` + strings.Join(parts, "\nUNION ALL\n") + `) x`
	if database != "" {
		query += ` WHERE dbName IN (?, ?)`
		args = append(args, database, bracketName(database))
	}
	var row struct {
		HistoryRows      int64      `gorm:"column:history_rows"`
		Errors24h        int64      `gorm:"column:errors_24h"`
		LastRunStart     *time.Time `gorm:"column:last_run_start"`
		LastRunEnd       *time.Time `gorm:"column:last_run_end"`
		LastError        *time.Time `gorm:"column:last_error"`
		LastErrorMessage string     `gorm:"column:last_error_message"`
	}
	if err := db.WithContext(ctx).Raw(query, args...).Scan(&row).Error; err != nil {
		return fmt.Errorf("query defrag log summary: %w", err)
	}
	out.HistoryRows = row.HistoryRows
	out.Errors24h = row.Errors24h
	out.LastRunStart = row.LastRunStart
	out.LastRunEnd = row.LastRunEnd
	out.LastError = row.LastError
	out.LastErrorMessage = row.LastErrorMessage
	return nil
}

func tableExists(ctx context.Context, db *gorm.DB, maintenanceDB, name string) bool {
	maintenanceDB = normalizeMaintenanceDatabase(maintenanceDB)
	var count int64
	err := db.WithContext(ctx).Raw(fmt.Sprintf(`
SELECT COUNT(1)
FROM %s t
JOIN %s s ON s.schema_id = t.schema_id
WHERE s.name = N'dbo' AND t.name = ?`,
		qualifyName(maintenanceDB, "sys", "tables"),
		qualifyName(maintenanceDB, "sys", "schemas"),
	), name).Scan(&count).Error
	return err == nil && count > 0
}

func ListRunningRuns(ctx context.Context, db *gorm.DB, opts StopOptions) ([]DefragSession, error) {
	maintenanceDB := normalizeMaintenanceDatabase(opts.MaintenanceDatabase)
	var sessions []DefragSession
	err := db.WithContext(ctx).Raw(`
SELECT
  r.session_id,
  r.status,
  DB_NAME(r.database_id) AS database_name,
  r.command,
  r.start_time AS started_at,
  COALESCE(CONVERT(NVARCHAR(MAX), ib.event_info), st.text) AS sql_text
FROM sys.dm_exec_requests r
OUTER APPLY sys.dm_exec_sql_text(r.sql_handle) st
OUTER APPLY sys.dm_exec_input_buffer(r.session_id, r.request_id) ib
WHERE r.session_id <> @@SPID
  AND (
    COALESCE(st.text, N'') LIKE N'%usp_AdaptiveIndexDefrag%'
    OR COALESCE(CONVERT(NVARCHAR(MAX), ib.event_info), N'') LIKE N'%usp_AdaptiveIndexDefrag%'
  )`).Scan(&sessions).Error
	if err != nil {
		return nil, fmt.Errorf("list AdaptiveIndexDefrag sessions: %w", err)
	}
	for i := range sessions {
		sessions[i].MaintenanceDatabase = maintenanceDB
	}
	return sessions, nil
}

func TerminateExistingRuns(ctx context.Context, db *gorm.DB, opts StopOptions) (TerminateResult, error) {
	maintenanceDB := normalizeMaintenanceDatabase(opts.MaintenanceDatabase)
	result := TerminateResult{MaintenanceDatabase: maintenanceDB}
	sessions, err := ListRunningRuns(ctx, db, StopOptions{MaintenanceDatabase: maintenanceDB})
	if err != nil {
		return result, err
	}
	for _, session := range sessions {
		if err := db.WithContext(ctx).Exec(fmt.Sprintf("KILL %d", session.SessionID)).Error; err != nil {
			result.Errors = append(result.Errors, RunMessage{
				Type:  "error",
				Error: fmt.Sprintf("kill session %d: %s", session.SessionID, err.Error()),
			})
			continue
		}
		result.Terminated = append(result.Terminated, session)
	}
	if len(result.Errors) > 0 {
		return result, fmt.Errorf("terminate %d AdaptiveIndexDefrag session(s): %s", len(result.Errors), result.Errors[0].Error)
	}
	return result, nil
}

func currentRequestCount(ctx context.Context, db *gorm.DB, maintenanceDB string) int64 {
	sessions, err := ListRunningRuns(ctx, db, StopOptions{MaintenanceDatabase: maintenanceDB})
	if err != nil {
		return 0
	}
	return int64(len(sessions))
}

func bit(v bool) int {
	if v {
		return 1
	}
	return 0
}

func nullableString(v string) any {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	return v
}

func nullablePositiveInt(v int) any {
	if v <= 0 {
		return nil
	}
	return v
}

func nullablePositiveInt64(v int64) any {
	if v <= 0 {
		return nil
	}
	return v
}

func nullablePositiveFloat(v float64) any {
	if v <= 0 {
		return nil
	}
	return v
}

func indexTypeOption(value string) (any, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "all":
		return nil, nil
	case "clustered":
		return 1, nil
	case "nonclustered", "non-clustered":
		return 0, nil
	default:
		return nil, fmt.Errorf("indexType must be all, clustered, or nonclustered")
	}
}

func tristateOption(name, value string) (any, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "keep", "unchanged", "default":
		return nil, nil
	case "on", "true", "1":
		return 1, nil
	case "off", "false", "0":
		return 0, nil
	default:
		return nil, fmt.Errorf("%s must be keep, on, or off", name)
	}
}

func partitionModeOption(value string) (any, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "rightmost":
		return 0, nil
	case "exclude-rightmost", "exclude_rightmost":
		return 1, nil
	case "all":
		return nil, nil
	default:
		return nil, fmt.Errorf("partitionMode must be rightmost, exclude-rightmost, or all")
	}
}

func abortAfterWaitOption(value string) (any, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "self":
		return 1, nil
	case "blockers":
		return 0, nil
	case "none":
		return nil, nil
	default:
		return nil, fmt.Errorf("abortAfterWait must be self, blockers, or none")
	}
}

func fillFactorOption(value string) (int, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "existing", "original":
		return 1, nil
	case "default":
		return 0, nil
	default:
		return 0, fmt.Errorf("fillFactor must be existing or default")
	}
}

func statsSampleOption(value string) (any, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	upper := strings.ToUpper(value)
	if upper == "FULLSCAN" || upper == "RESAMPLE" {
		return upper, nil
	}
	if !regexp.MustCompile(`^\d+(\.\d+)?$`).MatchString(value) {
		return nil, fmt.Errorf("statsSample must be empty, FULLSCAN, RESAMPLE, or a percentage from 1 to 100")
	}
	var pct float64
	if _, err := fmt.Sscanf(value, "%f", &pct); err != nil {
		return nil, fmt.Errorf("statsSample must be empty, FULLSCAN, RESAMPLE, or a percentage from 1 to 100")
	}
	if pct < 1 || pct > 100 {
		return nil, fmt.Errorf("statsSample percentage must be between 1 and 100")
	}
	return value, nil
}

func dataCompressionOption(value string) (any, error) {
	value = strings.ToUpper(strings.TrimSpace(value))
	switch value {
	case "":
		return nil, nil
	case "NONE", "ROW", "PAGE":
		return value, nil
	default:
		return nil, fmt.Errorf("dataCompression must be empty, NONE, ROW, or PAGE")
	}
}

func bracketName(name string) string {
	return "[" + strings.ReplaceAll(name, "]", "]]") + "]"
}

func normalizeMaintenanceDatabase(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return DefaultMaintenanceDatabase
	}
	return name
}

func qualifyName(database, schema, object string) string {
	return bracketName(normalizeMaintenanceDatabase(database)) + "." + bracketName(schema) + "." + bracketName(object)
}
