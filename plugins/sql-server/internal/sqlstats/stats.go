// Package sqlstats collects health snapshots from a SQL Server instance —
// instance/CPU/memory/disk/IO. Results are cached in-memory per-process,
// keyed by (server, database).
package sqlstats

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
)

const (
	cpuCacheTTL          = 5 * time.Second
	cpuSampleCacheMaxAge = 2 * time.Minute
	memoryCacheTTL       = 15 * time.Second
	diskCacheTTL         = 60 * time.Second
	instanceCacheTTL     = 24 * time.Hour
	ioCacheTTL           = 5 * time.Second
	ioSampleCacheMaxAge  = 2 * time.Minute
	collectionTimeout    = 12 * time.Second
)

// Response is the public stats payload. Each field is omitempty so the
// response shape degrades gracefully when individual collectors fail.
type Response struct {
	CapturedAt time.Time            `json:"capturedAt"`
	Instance   *InstanceStats       `json:"instance,omitempty"`
	CPU        *CPUStats            `json:"cpu,omitempty"`
	Memory     *MemoryStats         `json:"memory,omitempty"`
	Disk       *DiskStats           `json:"disk,omitempty"`
	IO         *IOStats             `json:"io,omitempty"`
	Cache      map[string]CacheInfo `json:"cache,omitempty"`
	Warnings   []string             `json:"warnings,omitempty"`
}

type InstanceStats struct {
	ServerName      string    `json:"serverName,omitempty"`
	DatabaseName    string    `json:"databaseName,omitempty"`
	ProductVersion  string    `json:"productVersion,omitempty"`
	Edition         string    `json:"edition,omitempty"`
	EngineEdition   int       `json:"engineEdition,omitempty"`
	CPUCount        int       `json:"cpuCount,omitempty"`
	SchedulerCount  int       `json:"schedulerCount,omitempty"`
	StartedAt       time.Time `json:"startedAt,omitempty"`
	UptimeSeconds   int64     `json:"uptimeSeconds,omitempty"`
	CapabilitiesKey string    `json:"capabilitiesKey,omitempty"`
}

type CPUStats struct {
	// Pending is true when only one snapshot is in cache so the delta can't
	// be computed yet — same convention as IOStats.
	Pending        bool      `json:"pending"`
	ProcessPercent float64   `json:"processPercent"`
	ElapsedSeconds float64   `json:"elapsedSeconds,omitempty"`
	SchedulerCount int       `json:"schedulerCount,omitempty"`
	CapturedAt     time.Time `json:"capturedAt,omitempty"`
	Source         string    `json:"source"`
}

type cpuSnapshot struct {
	CapturedAt time.Time
	CPUTicks   int64
	MSTicks    int64
}

type MemoryStats struct {
	PhysicalMemoryBytes      int64 `json:"physicalMemoryBytes,omitempty"`
	CommittedBytes           int64 `json:"committedBytes,omitempty"`
	CommittedTargetBytes     int64 `json:"committedTargetBytes,omitempty"`
	ProcessPhysicalBytes     int64 `json:"processPhysicalBytes,omitempty"`
	MemoryUtilizationPercent int   `json:"memoryUtilizationPercent,omitempty"`
	AvailablePhysicalBytes   int64 `json:"availablePhysicalBytes,omitempty"`
	TotalPhysicalBytes       int64 `json:"totalPhysicalBytes,omitempty"`
}

type DiskStats struct {
	TotalBytes    int64               `json:"totalBytes"`
	DataBytes     int64               `json:"dataBytes"`
	LogBytes      int64               `json:"logBytes"`
	TempdbBytes   int64               `json:"tempdbBytes"`
	DatabaseCount int                 `json:"databaseCount"`
	TopDatabases  []DatabaseFileStats `json:"topDatabases,omitempty"`
	Source        string              `json:"source"`
}

type DatabaseFileStats struct {
	DatabaseName string `json:"databaseName"`
	TotalBytes   int64  `json:"totalBytes"`
	DataBytes    int64  `json:"dataBytes"`
	LogBytes     int64  `json:"logBytes"`
}

type IOStats struct {
	Pending                bool      `json:"pending"`
	ElapsedSeconds         float64   `json:"elapsedSeconds,omitempty"`
	ReadIOPS               float64   `json:"readIops"`
	WriteIOPS              float64   `json:"writeIops"`
	ReadBytesPerSecond     float64   `json:"readBytesPerSecond"`
	WriteBytesPerSecond    float64   `json:"writeBytesPerSecond"`
	AverageReadLatencyMS   float64   `json:"averageReadLatencyMs,omitempty"`
	AverageWriteLatencyMS  float64   `json:"averageWriteLatencyMs,omitempty"`
	CumulativeReads        int64     `json:"cumulativeReads"`
	CumulativeWrites       int64     `json:"cumulativeWrites"`
	CumulativeBytesRead    int64     `json:"cumulativeBytesRead"`
	CumulativeBytesWritten int64     `json:"cumulativeBytesWritten"`
	SampleMS               int64     `json:"sampleMs,omitempty"`
	CapturedAt             time.Time `json:"capturedAt"`
	Source                 string    `json:"source"`
}

type CacheInfo struct {
	Hit        bool      `json:"hit"`
	SavedAt    time.Time `json:"savedAt,omitempty"`
	AgeSeconds float64   `json:"ageSeconds,omitempty"`
	TTLSeconds int64     `json:"ttlSeconds"`
}

type ioSnapshot struct {
	CapturedAt       time.Time
	Reads            int64
	Writes           int64
	BytesRead        int64
	BytesWritten     int64
	StallReadMS      int64
	StallWriteMS     int64
	SampleMS         int64
	DatabaseFileRows int
}

// Collect runs the per-group collectors against db, honouring the per-group
// TTLs from the in-process cache. Pass refresh=true to bypass the cache.
func Collect(parent context.Context, db *gorm.DB, refresh bool) (Response, error) {
	ctx, cancel := context.WithTimeout(parent, collectionTimeout)
	defer cancel()

	resp := Response{
		CapturedAt: time.Now(),
		Cache:      map[string]CacheInfo{},
	}

	identity, identityWarnings := cacheIdentity(ctx, db)
	resp.Warnings = append(resp.Warnings, identityWarnings...)
	c := newCache(identity)

	{
		instance, info, warnings, ok := loadOrCollect(c, "instance", instanceCacheTTL, refresh, func() (InstanceStats, []string, error) {
			return collectInstanceStats(ctx, db)
		})
		if ok {
			resp.Instance = &instance
		}
		resp.Cache["instance"] = info
		resp.Warnings = append(resp.Warnings, warnings...)
	}
	{
		cpu, info, warnings, ok := loadOrCollect(c, "cpu", cpuCacheTTL, refresh, func() (CPUStats, []string, error) {
			return collectCPUStats(ctx, db, c, refresh)
		})
		if ok {
			resp.CPU = &cpu
		}
		resp.Cache["cpu"] = info
		resp.Warnings = append(resp.Warnings, warnings...)
	}
	{
		memory, info, warnings, ok := loadOrCollect(c, "memory", memoryCacheTTL, refresh, func() (MemoryStats, []string, error) {
			return collectMemoryStats(ctx, db)
		})
		if ok {
			resp.Memory = &memory
		}
		resp.Cache["memory"] = info
		resp.Warnings = append(resp.Warnings, warnings...)
	}
	{
		disk, info, warnings, ok := loadOrCollect(c, "disk", diskCacheTTL, refresh, func() (DiskStats, []string, error) {
			return collectDiskStats(ctx, db)
		})
		if ok {
			resp.Disk = &disk
		}
		resp.Cache["disk"] = info
		resp.Warnings = append(resp.Warnings, warnings...)
	}
	{
		io, info, warnings, ok := loadOrCollect(c, "io", ioCacheTTL, refresh, func() (IOStats, []string, error) {
			return collectIOStats(ctx, db, c, refresh)
		})
		if ok {
			resp.IO = &io
		}
		resp.Cache["io"] = info
		resp.Warnings = append(resp.Warnings, warnings...)
	}

	if len(resp.Warnings) == 0 {
		resp.Warnings = nil
	}
	if len(resp.Cache) == 0 {
		resp.Cache = nil
	}
	return resp, nil
}

func loadOrCollect[T any](c statsCache, group string, ttl time.Duration, refresh bool, collect func() (T, []string, error)) (T, CacheInfo, []string, bool) {
	var zero T
	if !refresh {
		var cached T
		if info, ok := c.load(group, ttl, &cached); ok {
			return cached, info, nil, true
		}
	}
	value, warnings, err := collect()
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("%s unavailable: %v", group, err))
		return zero, CacheInfo{TTLSeconds: int64(ttl.Seconds())}, warnings, false
	}
	if len(warnings) == 0 {
		c.save(group, value)
	}
	return value, freshCacheInfo(ttl), warnings, true
}

func cacheIdentity(ctx context.Context, db *gorm.DB) (string, []string) {
	var row struct {
		ServerName   *string `gorm:"column:server_name"`
		DatabaseName *string `gorm:"column:database_name"`
	}
	err := db.WithContext(ctx).Raw(`
SELECT
  COALESCE(CONVERT(nvarchar(256), SERVERPROPERTY('ServerName')), @@SERVERNAME) AS server_name,
  DB_NAME() AS database_name
`).Scan(&row).Error
	if err != nil {
		return "", []string{"stats cache disabled: resolve SQL Server identity: " + err.Error()}
	}
	parts := []string{strings.TrimSpace(strDeref(row.ServerName)), strings.TrimSpace(strDeref(row.DatabaseName))}
	return strings.Join(parts, "/"), nil
}

func collectInstanceStats(ctx context.Context, db *gorm.DB) (InstanceStats, []string, error) {
	var row struct {
		ServerName     *string    `gorm:"column:server_name"`
		DatabaseName   *string    `gorm:"column:database_name"`
		ProductVersion *string    `gorm:"column:product_version"`
		Edition        *string    `gorm:"column:edition"`
		EngineEdition  *int       `gorm:"column:engine_edition"`
		CPUCount       *int       `gorm:"column:cpu_count"`
		SchedulerCount *int       `gorm:"column:scheduler_count"`
		StartedAt      *time.Time `gorm:"column:sqlserver_start_time"`
	}
	err := db.WithContext(ctx).Raw(`
SELECT
  COALESCE(CONVERT(nvarchar(256), SERVERPROPERTY('ServerName')), @@SERVERNAME) AS server_name,
  DB_NAME() AS database_name,
  CONVERT(nvarchar(128), SERVERPROPERTY('ProductVersion')) AS product_version,
  CONVERT(nvarchar(256), SERVERPROPERTY('Edition')) AS edition,
  CONVERT(int, SERVERPROPERTY('EngineEdition')) AS engine_edition,
  cpu_count,
  scheduler_count,
  sqlserver_start_time
FROM sys.dm_os_sys_info
`).Scan(&row).Error
	if err != nil {
		return InstanceStats{}, nil, fmt.Errorf("collect instance stats: %w", err)
	}
	out := InstanceStats{
		ServerName:     strDeref(row.ServerName),
		DatabaseName:   strDeref(row.DatabaseName),
		ProductVersion: strDeref(row.ProductVersion),
		Edition:        strDeref(row.Edition),
		EngineEdition:  intDeref(row.EngineEdition),
		CPUCount:       intDeref(row.CPUCount),
		SchedulerCount: intDeref(row.SchedulerCount),
	}
	if row.StartedAt != nil {
		out.StartedAt = *row.StartedAt
		out.UptimeSeconds = int64(time.Since(*row.StartedAt).Seconds())
		out.CapabilitiesKey = fmt.Sprintf("%s/%s", out.ProductVersion, out.StartedAt.Format(time.RFC3339))
	}
	return out, nil, nil
}

// collectCPUStats samples sys.dm_os_sys_info's cpu_ticks / ms_ticks, caches
// the snapshot, and returns the delta against the previous one. The first
// call returns Pending=true (no baseline yet); subsequent calls give the
// real percentage.
//
// SQL CPU% formula (independent of TSC frequency / scheduler count):
//
//	pct = (Δcpu_ticks / Δms_ticks) / (cpu_ticks_now / ms_ticks_now) × 100
//
// The denominator is the cumulative ticks-per-ms ratio, which equals the
// CPU TSC frequency × scheduler count — i.e. the maximum possible cpu_ticks
// per ms across all schedulers. Dividing the per-interval ratio by it gives
// utilisation in [0,1]. Works on Windows and Linux.
//
// Replaces the older RING_BUFFER_SCHEDULER_MONITOR query which is unreliable
// on Linux/containers (the buffer is often empty) and lossy elsewhere.
func collectCPUStats(ctx context.Context, db *gorm.DB, c statsCache, refresh bool) (CPUStats, []string, error) {
	current, err := collectCPUSnapshot(ctx, db)
	if err != nil {
		return CPUStats{}, nil, err
	}

	out := CPUStats{
		Pending:    true,
		CapturedAt: current.CapturedAt,
		Source:     "sys.dm_os_sys_info cpu_ticks delta",
	}

	var previous cpuSnapshot
	if !refresh {
		if _, ok := c.load("cpu-sample", cpuSampleCacheMaxAge, &previous); ok {
			out = computeCPUDelta(previous, current)
		}
	}
	c.save("cpu-sample", current)
	return out, nil, nil
}

func collectCPUSnapshot(ctx context.Context, db *gorm.DB) (cpuSnapshot, error) {
	var row struct {
		CPUTicks *int64 `gorm:"column:cpu_ticks"`
		MSTicks  *int64 `gorm:"column:ms_ticks"`
	}
	err := db.WithContext(ctx).Raw(`
SELECT cpu_ticks, ms_ticks
FROM sys.dm_os_sys_info
`).Scan(&row).Error
	if err != nil {
		return cpuSnapshot{}, fmt.Errorf("collect cpu stats: %w", err)
	}
	if row.CPUTicks == nil || row.MSTicks == nil {
		return cpuSnapshot{}, fmt.Errorf("collect cpu stats: dm_os_sys_info returned no ticks")
	}
	return cpuSnapshot{
		CapturedAt: time.Now(),
		CPUTicks:   *row.CPUTicks,
		MSTicks:    *row.MSTicks,
	}, nil
}

func computeCPUDelta(prev, current cpuSnapshot) CPUStats {
	out := CPUStats{
		Pending:    true,
		CapturedAt: current.CapturedAt,
		Source:     "sys.dm_os_sys_info cpu_ticks delta",
	}
	dCPU := current.CPUTicks - prev.CPUTicks
	dMS := current.MSTicks - prev.MSTicks
	elapsed := current.CapturedAt.Sub(prev.CapturedAt).Seconds()
	// Cumulative ratio = total ticks per ms across all schedulers (TSC freq
	// × scheduler count). Pulled from the latest sample so we don't need to
	// know either independently.
	if dCPU < 0 || dMS <= 0 || current.MSTicks <= 0 || current.CPUTicks <= 0 {
		return out
	}
	if elapsed <= 0 || elapsed > cpuSampleCacheMaxAge.Seconds() {
		return out
	}
	cumulativeRatio := float64(current.CPUTicks) / float64(current.MSTicks)
	if cumulativeRatio <= 0 {
		return out
	}
	intervalRatio := float64(dCPU) / float64(dMS)
	pct := (intervalRatio / cumulativeRatio) * 100
	out.Pending = false
	out.ProcessPercent = clampPercent(pct)
	out.ElapsedSeconds = elapsed
	return out
}

func collectMemoryStats(ctx context.Context, db *gorm.DB) (MemoryStats, []string, error) {
	var warnings []string
	out := MemoryStats{}
	success := false

	var sysInfo struct {
		PhysicalMemoryKB  *int64 `gorm:"column:physical_memory_kb"`
		CommittedKB       *int64 `gorm:"column:committed_kb"`
		CommittedTargetKB *int64 `gorm:"column:committed_target_kb"`
	}
	if err := db.WithContext(ctx).Raw(`
SELECT physical_memory_kb, committed_kb, committed_target_kb
FROM sys.dm_os_sys_info
`).Scan(&sysInfo).Error; err != nil {
		warnings = append(warnings, "memory sys info unavailable: "+err.Error())
	} else {
		success = true
		out.PhysicalMemoryBytes = kbToBytes(int64Deref(sysInfo.PhysicalMemoryKB))
		out.CommittedBytes = kbToBytes(int64Deref(sysInfo.CommittedKB))
		out.CommittedTargetBytes = kbToBytes(int64Deref(sysInfo.CommittedTargetKB))
	}

	var proc struct {
		PhysicalMemoryInUseKB    *int64 `gorm:"column:physical_memory_in_use_kb"`
		MemoryUtilizationPercent *int   `gorm:"column:memory_utilization_percentage"`
	}
	if err := db.WithContext(ctx).Raw(`
SELECT
  physical_memory_in_use_kb,
  memory_utilization_percentage
FROM sys.dm_os_process_memory
`).Scan(&proc).Error; err != nil {
		warnings = append(warnings, "process memory unavailable: "+err.Error())
	} else {
		success = true
		out.ProcessPhysicalBytes = kbToBytes(int64Deref(proc.PhysicalMemoryInUseKB))
		out.MemoryUtilizationPercent = intDeref(proc.MemoryUtilizationPercent)
	}

	var sysMem struct {
		TotalPhysicalMemoryKB     *int64 `gorm:"column:total_physical_memory_kb"`
		AvailablePhysicalMemoryKB *int64 `gorm:"column:available_physical_memory_kb"`
	}
	if err := db.WithContext(ctx).Raw(`
SELECT total_physical_memory_kb, available_physical_memory_kb
FROM sys.dm_os_sys_memory
`).Scan(&sysMem).Error; err != nil {
		warnings = append(warnings, "host memory unavailable: "+err.Error())
	} else {
		success = true
		out.TotalPhysicalBytes = kbToBytes(int64Deref(sysMem.TotalPhysicalMemoryKB))
		out.AvailablePhysicalBytes = kbToBytes(int64Deref(sysMem.AvailablePhysicalMemoryKB))
	}

	if !success {
		return MemoryStats{}, warnings, fmt.Errorf("collect memory stats: no memory DMV query succeeded")
	}
	return out, warnings, nil
}

func collectDiskStats(ctx context.Context, db *gorm.DB) (DiskStats, []string, error) {
	var aggregate struct {
		TotalBytes    *int64 `gorm:"column:total_bytes"`
		DataBytes     *int64 `gorm:"column:data_bytes"`
		LogBytes      *int64 `gorm:"column:log_bytes"`
		TempdbBytes   *int64 `gorm:"column:tempdb_bytes"`
		DatabaseCount *int   `gorm:"column:database_count"`
	}
	if err := db.WithContext(ctx).Raw(`
SELECT
  SUM(CAST(size AS bigint) * 8192) AS total_bytes,
  SUM(CASE WHEN database_id <> 2 AND type_desc = 'ROWS' THEN CAST(size AS bigint) * 8192 ELSE 0 END) AS data_bytes,
  SUM(CASE WHEN database_id <> 2 AND type_desc = 'LOG' THEN CAST(size AS bigint) * 8192 ELSE 0 END) AS log_bytes,
  SUM(CASE WHEN database_id = 2 THEN CAST(size AS bigint) * 8192 ELSE 0 END) AS tempdb_bytes,
  COUNT(DISTINCT database_id) AS database_count
FROM sys.master_files
`).Scan(&aggregate).Error; err != nil {
		return DiskStats{}, nil, fmt.Errorf("collect disk stats: %w", err)
	}

	var topRows []struct {
		DatabaseName *string `gorm:"column:database_name"`
		TotalBytes   *int64  `gorm:"column:total_bytes"`
		DataBytes    *int64  `gorm:"column:data_bytes"`
		LogBytes     *int64  `gorm:"column:log_bytes"`
	}
	var warnings []string
	if err := db.WithContext(ctx).Raw(`
SELECT TOP (8)
  DB_NAME(database_id) AS database_name,
  SUM(CAST(size AS bigint) * 8192) AS total_bytes,
  SUM(CASE WHEN type_desc = 'ROWS' THEN CAST(size AS bigint) * 8192 ELSE 0 END) AS data_bytes,
  SUM(CASE WHEN type_desc = 'LOG' THEN CAST(size AS bigint) * 8192 ELSE 0 END) AS log_bytes
FROM sys.master_files
GROUP BY database_id
ORDER BY SUM(CAST(size AS bigint) * 8192) DESC
`).Scan(&topRows).Error; err != nil {
		warnings = append(warnings, "top database file sizes unavailable: "+err.Error())
	}
	top := make([]DatabaseFileStats, 0, len(topRows))
	for _, r := range topRows {
		top = append(top, DatabaseFileStats{
			DatabaseName: strDeref(r.DatabaseName),
			TotalBytes:   int64Deref(r.TotalBytes),
			DataBytes:    int64Deref(r.DataBytes),
			LogBytes:     int64Deref(r.LogBytes),
		})
	}
	return DiskStats{
		TotalBytes:    int64Deref(aggregate.TotalBytes),
		DataBytes:     int64Deref(aggregate.DataBytes),
		LogBytes:      int64Deref(aggregate.LogBytes),
		TempdbBytes:   int64Deref(aggregate.TempdbBytes),
		DatabaseCount: intDeref(aggregate.DatabaseCount),
		TopDatabases:  top,
		Source:        "sys.master_files allocated database/log/tempdb file sizes",
	}, warnings, nil
}

func collectIOStats(ctx context.Context, db *gorm.DB, c statsCache, refresh bool) (IOStats, []string, error) {
	current, err := collectIOSnapshot(ctx, db)
	if err != nil {
		return IOStats{}, nil, err
	}
	out := ioStatsFromSnapshot(current)
	out.Pending = true

	var previous ioSnapshot
	if !refresh {
		if _, ok := c.load("io-sample", ioSampleCacheMaxAge, &previous); ok {
			out = computeIODelta(previous, current)
		}
	}
	c.save("io-sample", current)
	return out, nil, nil
}

func collectIOSnapshot(ctx context.Context, db *gorm.DB) (ioSnapshot, error) {
	var row struct {
		Reads            *int64 `gorm:"column:reads"`
		Writes           *int64 `gorm:"column:writes"`
		BytesRead        *int64 `gorm:"column:bytes_read"`
		BytesWritten     *int64 `gorm:"column:bytes_written"`
		StallReadMS      *int64 `gorm:"column:stall_read_ms"`
		StallWriteMS     *int64 `gorm:"column:stall_write_ms"`
		SampleMS         *int64 `gorm:"column:sample_ms"`
		DatabaseFileRows *int   `gorm:"column:database_file_rows"`
	}
	err := db.WithContext(ctx).Raw(`
SELECT
  SUM(num_of_reads) AS reads,
  SUM(num_of_writes) AS writes,
  SUM(num_of_bytes_read) AS bytes_read,
  SUM(num_of_bytes_written) AS bytes_written,
  SUM(io_stall_read_ms) AS stall_read_ms,
  SUM(io_stall_write_ms) AS stall_write_ms,
  MAX(sample_ms) AS sample_ms,
  COUNT(*) AS database_file_rows
FROM sys.dm_io_virtual_file_stats(NULL, NULL)
`).Scan(&row).Error
	if err != nil {
		return ioSnapshot{}, fmt.Errorf("collect io stats: %w", err)
	}
	return ioSnapshot{
		CapturedAt:       time.Now(),
		Reads:            int64Deref(row.Reads),
		Writes:           int64Deref(row.Writes),
		BytesRead:        int64Deref(row.BytesRead),
		BytesWritten:     int64Deref(row.BytesWritten),
		StallReadMS:      int64Deref(row.StallReadMS),
		StallWriteMS:     int64Deref(row.StallWriteMS),
		SampleMS:         int64Deref(row.SampleMS),
		DatabaseFileRows: intDeref(row.DatabaseFileRows),
	}, nil
}

func ioStatsFromSnapshot(s ioSnapshot) IOStats {
	return IOStats{
		Pending:                true,
		CumulativeReads:        s.Reads,
		CumulativeWrites:       s.Writes,
		CumulativeBytesRead:    s.BytesRead,
		CumulativeBytesWritten: s.BytesWritten,
		SampleMS:               s.SampleMS,
		CapturedAt:             s.CapturedAt,
		Source:                 "sys.dm_io_virtual_file_stats",
	}
}

func computeIODelta(prev, current ioSnapshot) IOStats {
	out := ioStatsFromSnapshot(current)
	elapsed := current.CapturedAt.Sub(prev.CapturedAt).Seconds()
	if elapsed <= 0 || elapsed > ioSampleCacheMaxAge.Seconds() {
		out.Pending = true
		return out
	}
	readDelta := current.Reads - prev.Reads
	writeDelta := current.Writes - prev.Writes
	bytesReadDelta := current.BytesRead - prev.BytesRead
	bytesWrittenDelta := current.BytesWritten - prev.BytesWritten
	stallReadDelta := current.StallReadMS - prev.StallReadMS
	stallWriteDelta := current.StallWriteMS - prev.StallWriteMS
	if readDelta < 0 || writeDelta < 0 || bytesReadDelta < 0 || bytesWrittenDelta < 0 || stallReadDelta < 0 || stallWriteDelta < 0 {
		out.Pending = true
		return out
	}
	out.Pending = false
	out.ElapsedSeconds = elapsed
	out.ReadIOPS = float64(readDelta) / elapsed
	out.WriteIOPS = float64(writeDelta) / elapsed
	out.ReadBytesPerSecond = float64(bytesReadDelta) / elapsed
	out.WriteBytesPerSecond = float64(bytesWrittenDelta) / elapsed
	if readDelta > 0 {
		out.AverageReadLatencyMS = float64(stallReadDelta) / float64(readDelta)
	}
	if writeDelta > 0 {
		out.AverageWriteLatencyMS = float64(stallWriteDelta) / float64(writeDelta)
	}
	return out
}

// statsCache is an in-memory per-process cache of collected groups, keyed by
// (server, database).
type statsCache struct {
	identity string
}

type cacheEntry struct {
	savedAt time.Time
	data    json.RawMessage
}

var (
	cacheMu      sync.Mutex
	cacheEntries = map[string]cacheEntry{}
)

func newCache(identity string) statsCache {
	return statsCache{identity: identity}
}

func (c statsCache) key(group string) string {
	if c.identity == "" || group == "" {
		return ""
	}
	return c.identity + "|" + group
}

func (c statsCache) load(group string, ttl time.Duration, out any) (CacheInfo, bool) {
	info := CacheInfo{TTLSeconds: int64(ttl.Seconds())}
	k := c.key(group)
	if k == "" {
		return info, false
	}
	cacheMu.Lock()
	entry, ok := cacheEntries[k]
	cacheMu.Unlock()
	if !ok {
		return info, false
	}
	age := time.Since(entry.savedAt)
	if age > ttl {
		return CacheInfo{SavedAt: entry.savedAt, AgeSeconds: age.Seconds(), TTLSeconds: int64(ttl.Seconds())}, false
	}
	if err := json.Unmarshal(entry.data, out); err != nil {
		return info, false
	}
	return CacheInfo{Hit: true, SavedAt: entry.savedAt, AgeSeconds: age.Seconds(), TTLSeconds: int64(ttl.Seconds())}, true
}

func (c statsCache) save(group string, value any) {
	k := c.key(group)
	if k == "" {
		return
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return
	}
	cacheMu.Lock()
	cacheEntries[k] = cacheEntry{savedAt: time.Now(), data: payload}
	cacheMu.Unlock()
}

func freshCacheInfo(ttl time.Duration) CacheInfo {
	return CacheInfo{Hit: false, SavedAt: time.Now(), TTLSeconds: int64(ttl.Seconds())}
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

func strDeref(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func kbToBytes(v int64) int64 {
	return v * 1024
}

func clampPercent(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}
