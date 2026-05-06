package pgvacuum

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
)

type TableStats struct {
	Schema           string    `json:"schema"`
	Table            string    `json:"table"`
	SizeBytes        int64     `json:"sizeBytes"`
	LiveTuples       int64     `json:"liveTuples"`
	DeadTuples       int64     `json:"deadTuples"`
	DeadTuplePct     float64   `json:"deadTuplePct"`
	LastVacuum       time.Time `json:"lastVacuum,omitempty"`
	LastAutovacuum   time.Time `json:"lastAutovacuum,omitempty"`
	LastAnalyze      time.Time `json:"lastAnalyze,omitempty"`
	LastAutoanalyze  time.Time `json:"lastAutoanalyze,omitempty"`
	VacuumCount      int64     `json:"vacuumCount"`
	AutovacuumCount  int64     `json:"autovacuumCount"`
	AnalyzeCount     int64     `json:"analyzeCount"`
	AutoanalyzeCount int64     `json:"autoanalyzeCount"`
}

type row struct {
	Schema           string     `gorm:"column:schema_name"`
	Table            string     `gorm:"column:table_name"`
	SizeBytes        *int64     `gorm:"column:size_bytes"`
	LiveTuples       *int64     `gorm:"column:live_tuples"`
	DeadTuples       *int64     `gorm:"column:dead_tuples"`
	DeadTuplePct     *float64   `gorm:"column:dead_tuple_pct"`
	LastVacuum       *time.Time `gorm:"column:last_vacuum"`
	LastAutovacuum   *time.Time `gorm:"column:last_autovacuum"`
	LastAnalyze      *time.Time `gorm:"column:last_analyze"`
	LastAutoanalyze  *time.Time `gorm:"column:last_autoanalyze"`
	VacuumCount      *int64     `gorm:"column:vacuum_count"`
	AutovacuumCount  *int64     `gorm:"column:autovacuum_count"`
	AnalyzeCount     *int64     `gorm:"column:analyze_count"`
	AutoanalyzeCount *int64     `gorm:"column:autoanalyze_count"`
}

func List(ctx context.Context, db *gorm.DB, limit int) ([]TableStats, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var rows []row
	if err := db.WithContext(ctx).Raw(`
SELECT schemaname AS schema_name, relname AS table_name,
       pg_total_relation_size(relid) AS size_bytes,
       n_live_tup AS live_tuples,
       n_dead_tup AS dead_tuples,
       CASE WHEN n_live_tup + n_dead_tup = 0 THEN 0
            ELSE (n_dead_tup::float8 / (n_live_tup + n_dead_tup)) * 100 END AS dead_tuple_pct,
       last_vacuum, last_autovacuum, last_analyze, last_autoanalyze,
       vacuum_count, autovacuum_count, analyze_count, autoanalyze_count
FROM pg_stat_user_tables
ORDER BY n_dead_tup DESC, pg_total_relation_size(relid) DESC
LIMIT ?
`, limit).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list vacuum stats: %w", err)
	}
	out := make([]TableStats, 0, len(rows))
	for _, r := range rows {
		out = append(out, TableStats{
			Schema:           r.Schema,
			Table:            r.Table,
			SizeBytes:        int64Deref(r.SizeBytes),
			LiveTuples:       int64Deref(r.LiveTuples),
			DeadTuples:       int64Deref(r.DeadTuples),
			DeadTuplePct:     float64Deref(r.DeadTuplePct),
			LastVacuum:       timeDeref(r.LastVacuum),
			LastAutovacuum:   timeDeref(r.LastAutovacuum),
			LastAnalyze:      timeDeref(r.LastAnalyze),
			LastAutoanalyze:  timeDeref(r.LastAutoanalyze),
			VacuumCount:      int64Deref(r.VacuumCount),
			AutovacuumCount:  int64Deref(r.AutovacuumCount),
			AnalyzeCount:     int64Deref(r.AnalyzeCount),
			AutoanalyzeCount: int64Deref(r.AutoanalyzeCount),
		})
	}
	return out, nil
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

func timeDeref(v *time.Time) time.Time {
	if v == nil {
		return time.Time{}
	}
	return *v
}
