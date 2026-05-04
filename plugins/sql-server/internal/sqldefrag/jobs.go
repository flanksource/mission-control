package sqldefrag

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
)

type JobStatus string

const (
	JobRunning JobStatus = "running"
	JobDone    JobStatus = "done"
	JobFailed  JobStatus = "failed"
	JobStopped JobStatus = "stopped"
)

type Job struct {
	ID           string             `json:"id"`
	Status       JobStatus          `json:"status"`
	Database     string             `json:"database,omitempty"`
	Table        string             `json:"table,omitempty"`
	StartedAt    time.Time          `json:"startedAt"`
	FinishedAt   *time.Time         `json:"finishedAt,omitempty"`
	Duration     time.Duration      `json:"duration,omitempty"`
	Error        string             `json:"error,omitempty"`
	HistoryError string             `json:"historyError,omitempty"`
	Result       *RunResult         `json:"result,omitempty"`
	Summary      JobSummary         `json:"summary"`
	History      []HistoryRow       `json:"history,omitempty"`
	Cancel       context.CancelFunc `json:"-"`
}

type JobSummary struct {
	Rows        int `json:"rows"`
	Indexes     int `json:"indexes"`
	Statistics  int `json:"statistics"`
	Rebuilds    int `json:"rebuilds"`
	Reorganizes int `json:"reorganizes"`
	Errors      int `json:"errors"`
}

type JobRegistry struct {
	mu   sync.Mutex
	jobs map[string]*Job
	db   DBProvider
}

// NewJobRegistry builds a JobRegistry. The DBProvider may be nil; in that
// case all callers must use StartWithDB so the registry never needs to
// resolve a default db itself.
func NewJobRegistry(db DBProvider) *JobRegistry {
	return &JobRegistry{jobs: map[string]*Job{}, db: db}
}

func (r *JobRegistry) Start(opts RunOptions) (*Job, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("job registry is not configured")
	}
	db, err := r.db()
	if err != nil {
		return nil, fmt.Errorf("database unavailable: %w", err)
	}
	return r.startWithDB(db, opts)
}

// StartWithDB starts a job against the given DB. Used by callers (like the
// mission-control plugin) that already hold a per-target-db handle and don't
// want the registry-wide DBProvider to apply.
func (r *JobRegistry) StartWithDB(db *gorm.DB, opts RunOptions) (*Job, error) {
	if r == nil {
		return nil, fmt.Errorf("job registry is not configured")
	}
	if db == nil {
		return nil, fmt.Errorf("db is required")
	}
	return r.startWithDB(db, opts)
}

func (r *JobRegistry) startWithDB(db *gorm.DB, opts RunOptions) (*Job, error) {
	normalized, err := normalizeRunOptions(context.Background(), db, opts)
	if err != nil {
		return nil, err
	}
	if _, _, err := BuildRunSQL(normalized); err != nil {
		return nil, err
	}
	if opts.StopExisting {
		r.StopRunning()
		normalized.StopExisting = false
	}
	if normalized.TerminateExisting {
		if _, err := TerminateExistingRuns(context.Background(), db, StopOptions{MaintenanceDatabase: normalized.MaintenanceDatabase}); err != nil {
			return nil, err
		}
		normalized.TerminateExisting = false
	}
	ctx, cancel := context.WithCancel(context.Background())

	job := &Job{
		ID:        fmt.Sprintf("defrag-%d", time.Now().UnixNano()),
		Status:    JobRunning,
		Database:  normalized.Database,
		Table:     normalized.Table,
		StartedAt: time.Now(),
		Cancel:    cancel,
	}
	r.mu.Lock()
	r.jobs[job.ID] = job
	r.pruneLocked(25)
	r.mu.Unlock()

	go r.runDetached(ctx, db, job.ID, normalized)
	return job.Clone(), nil
}

func (r *JobRegistry) runDetached(ctx context.Context, db *gorm.DB, id string, opts RunOptions) {
	started := time.Now()
	r.mu.Lock()
	if job, ok := r.jobs[id]; ok {
		started = job.StartedAt
	}
	r.mu.Unlock()

	result, err := Run(ctx, db, opts)
	finished := time.Now()
	history, historyErr := collectJobHistory(db, opts, started, finished)
	summary := summarizeHistory(history)

	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.jobs[id]
	if !ok {
		return
	}
	job.FinishedAt = &finished
	job.Duration = finished.Sub(job.StartedAt)
	job.Cancel = nil
	job.History = history
	job.Summary = summary
	if historyErr != nil {
		job.HistoryError = historyErr.Error()
	}
	if job.Status == JobStopped {
		if err != nil {
			job.Error = err.Error()
			job.Result = &result
		}
		return
	}
	if err != nil {
		job.Status = JobFailed
		job.Error = err.Error()
		job.Result = &result
		return
	}
	job.Status = JobDone
	job.Result = &result
}

func (r *JobRegistry) List() []*Job {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*Job, 0, len(r.jobs))
	for _, job := range r.jobs {
		out = append(out, job.Clone())
	}
	return out
}

func (r *JobRegistry) Get(id string) (*Job, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.jobs[id]
	if !ok {
		return nil, false
	}
	return job.Clone(), true
}

func (r *JobRegistry) Stop(id string) (*Job, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.jobs[id]
	if !ok {
		return nil, false
	}
	if job.Status == JobRunning {
		finished := time.Now()
		job.Status = JobStopped
		job.FinishedAt = &finished
		job.Duration = finished.Sub(job.StartedAt)
		if job.Cancel != nil {
			job.Cancel()
			job.Cancel = nil
		}
	}
	return job.Clone(), true
}

func (r *JobRegistry) StopRunning() []*Job {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var stopped []*Job
	for _, job := range r.jobs {
		if job.Status != JobRunning {
			continue
		}
		finished := time.Now()
		job.Status = JobStopped
		job.FinishedAt = &finished
		job.Duration = finished.Sub(job.StartedAt)
		if job.Cancel != nil {
			job.Cancel()
			job.Cancel = nil
		}
		stopped = append(stopped, job.Clone())
	}
	return stopped
}

func (r *JobRegistry) pruneLocked(keep int) {
	if len(r.jobs) <= keep {
		return
	}
	type candidate struct {
		id string
		at time.Time
	}
	var done []candidate
	for id, job := range r.jobs {
		if job.Status == JobRunning {
			continue
		}
		at := job.StartedAt
		if job.FinishedAt != nil {
			at = *job.FinishedAt
		}
		done = append(done, candidate{id: id, at: at})
	}
	for len(r.jobs) > keep && len(done) > 0 {
		oldest := 0
		for i := 1; i < len(done); i++ {
			if done[i].at.Before(done[oldest].at) {
				oldest = i
			}
		}
		delete(r.jobs, done[oldest].id)
		done = append(done[:oldest], done[oldest+1:]...)
	}
}

func (j *Job) Clone() *Job {
	if j == nil {
		return nil
	}
	cp := *j
	if j.FinishedAt != nil {
		t := *j.FinishedAt
		cp.FinishedAt = &t
	}
	if j.Result != nil {
		res := *j.Result
		cp.Result = &res
	}
	if j.History != nil {
		cp.History = append([]HistoryRow(nil), j.History...)
	}
	cp.Cancel = nil
	return &cp
}

func collectJobHistory(db *gorm.DB, opts RunOptions, started, finished time.Time) ([]HistoryRow, error) {
	rows, err := History(context.Background(), db, HistoryOptions{
		Database:            opts.Database,
		MaintenanceDatabase: opts.MaintenanceDatabase,
		Limit:               500,
	})
	if err != nil {
		return nil, err
	}
	out := make([]HistoryRow, 0, len(rows))
	for _, row := range rows {
		if row.StartedAt.Before(started.Add(-1*time.Second)) || row.StartedAt.After(finished.Add(1*time.Second)) {
			continue
		}
		if opts.Table != "" && !historyRowMatchesTable(row, opts.Table) {
			continue
		}
		out = append(out, row)
	}
	return out, nil
}

func historyRowMatchesTable(row HistoryRow, table string) bool {
	table = strings.ToLower(strings.TrimSpace(table))
	objectName := strings.ToLower(strings.TrimSpace(row.ObjectName))
	if table == "" || objectName == "" {
		return true
	}
	return objectName == table || strings.HasSuffix(table, "."+objectName)
}

func summarizeHistory(rows []HistoryRow) JobSummary {
	var out JobSummary
	out.Rows = len(rows)
	for _, row := range rows {
		if row.Error != "" {
			out.Errors++
		}
		switch strings.ToLower(row.Operation) {
		case "updatestats":
			out.Statistics++
		case "rebuild":
			out.Indexes++
			out.Rebuilds++
		case "reorg":
			out.Indexes++
			out.Reorganizes++
		default:
			if row.StatsName != "" {
				out.Statistics++
			} else {
				out.Indexes++
			}
		}
	}
	return out
}
