package db

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Job struct {
	ID         string     `json:"id"`
	Pattern    string     `json:"pattern"`
	IsRegex    bool       `json:"is_regex"`
	Status     string     `json:"status"`
	MatchCount *int       `json:"match_count,omitempty"`
	Error      *string    `json:"error,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Position   int64      `json:"position"`
}

var ErrQueueFull = errors.New("queue full")

func (d *DB) QueueDepth(ctx context.Context) (int, error) {
	var n int
	err := d.Pool.QueryRow(ctx, `SELECT count(*) FROM query_jobs WHERE status = 'queued'`).Scan(&n)
	return n, err
}

func (d *DB) JobHistory(ctx context.Context, limit, offset int) ([]*Job, error) {
	rows, err := d.Pool.Query(ctx, `
        SELECT id, pattern, is_regex, status, match_count, error,
               created_at, started_at, finished_at, position
          FROM query_jobs
         ORDER BY position DESC
         LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	jobs := []*Job{}
	for rows.Next() {
		j := &Job{}
		var mc *int
		var errStr *string
		var startedAt, finishedAt *time.Time
		if err := rows.Scan(&j.ID, &j.Pattern, &j.IsRegex, &j.Status, &mc, &errStr,
			&j.CreatedAt, &startedAt, &finishedAt, &j.Position); err != nil {
			return nil, err
		}
		j.MatchCount, j.Error, j.StartedAt, j.FinishedAt = mc, errStr, startedAt, finishedAt
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

func (d *DB) EnqueueJob(ctx context.Context, pattern string, isRegex bool, maxQueue int) (string, error) {
	depth, err := d.QueueDepth(ctx)
	if err != nil {
		return "", err
	}
	if depth >= maxQueue {
		return "", ErrQueueFull
	}
	var id string
	err = d.Pool.QueryRow(ctx, `
        INSERT INTO query_jobs (id, pattern, is_regex) VALUES ($1, $2, $3)
        RETURNING id`,
		uuidV4(), pattern, isRegex).Scan(&id)
	if err != nil {
		return "", err
	}
	return id, nil
}

// ClaimNextJob atomically marks the oldest queued job as running.
// Returns (nil, nil) when no queued job exists.
func (d *DB) ClaimNextJob(ctx context.Context) (*Job, error) {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	j := &Job{}
	err = tx.QueryRow(ctx, `
        UPDATE query_jobs
           SET status = 'running', started_at = now()
         WHERE id = (SELECT id FROM query_jobs
                      WHERE status = 'queued'
                      ORDER BY position
                      LIMIT 1
                      FOR UPDATE SKIP LOCKED)
        RETURNING id, pattern, is_regex, position`).Scan(&j.ID, &j.Pattern, &j.IsRegex, &j.Position)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	j.Status = "running"
	return j, nil
}

func (d *DB) SetJobPID(ctx context.Context, id string, pid int32) error {
	_, err := d.Pool.Exec(ctx, `UPDATE query_jobs SET pid = $1 WHERE id = $2`, pid, id)
	return err
}

func (d *DB) CompleteJob(ctx context.Context, id string, matchCount int) error {
	ct, err := d.Pool.Exec(ctx, `
        UPDATE query_jobs
           SET status = 'completed', match_count = $2, pid = NULL
         WHERE id = $1 AND status = 'running'`, id, matchCount)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return errors.New("job no longer running (likely cancelled)")
	}
	return nil
}

func (d *DB) FailJob(ctx context.Context, id, errMsg string) error {
	_, err := d.Pool.Exec(ctx, `
        UPDATE query_jobs
           SET status = 'failed', error = $2, pid = NULL
         WHERE id = $1 AND status = 'running'`, id, errMsg)
	return err
}

func (d *DB) CancelJob(ctx context.Context, id string) (wasRunning bool, pid int32, err error) {
	var status string
	err = d.Pool.QueryRow(ctx, `SELECT status, COALESCE(pid,0) FROM query_jobs WHERE id = $1`, id).Scan(&status, &pid)
	if err != nil {
		return false, 0, err
	}
	if status != "queued" && status != "running" {
		return false, 0, nil // nothing to cancel
	}
	_, err = d.Pool.Exec(ctx, `UPDATE query_jobs SET status = 'cancelled', pid = NULL WHERE id = $1`, id)
	if err != nil {
		return false, 0, err
	}
	return status == "running", pid, nil
}

func (d *DB) GetJob(ctx context.Context, id string) (*Job, error) {
	j := &Job{}
	var mc *int
	var errStr *string
	var startedAt, finishedAt *time.Time
	err := d.Pool.QueryRow(ctx, `
        SELECT id, pattern, is_regex, status, match_count, error,
               created_at, started_at, finished_at, position
          FROM query_jobs WHERE id = $1`, id).
		Scan(&j.ID, &j.Pattern, &j.IsRegex, &j.Status, &mc, &errStr,
			&j.CreatedAt, &startedAt, &finishedAt, &j.Position)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	j.MatchCount, j.Error, j.StartedAt, j.FinishedAt = mc, errStr, startedAt, finishedAt
	return j, nil
}

// Board returns the currently-running job (if any) and the waiting queue in order.
func (d *DB) Board(ctx context.Context) (running *Job, queued []*Job, err error) {
	queued = []*Job{} // non-nil so JSON encodes as [], not null
	// running
	r := &Job{}
	var mc *int
	var errStr *string
	var startedAt, finishedAt *time.Time
	err = d.Pool.QueryRow(ctx, `
        SELECT id, pattern, is_regex, status, match_count, error,
               created_at, started_at, finished_at, position
          FROM query_jobs WHERE status = 'running' LIMIT 1`).
		Scan(&r.ID, &r.Pattern, &r.IsRegex, &r.Status, &mc, &errStr,
			&r.CreatedAt, &startedAt, &finishedAt, &r.Position)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, err
	}
	if err == nil {
		r.MatchCount, r.Error, r.StartedAt, r.FinishedAt = mc, errStr, startedAt, finishedAt
		running = r
	}

	rows, err := d.Pool.Query(ctx, `
        SELECT id, pattern, is_regex, created_at, position
          FROM query_jobs WHERE status = 'queued' ORDER BY position`)
	if err != nil {
		return running, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		q := &Job{Status: "queued"}
		if err := rows.Scan(&q.ID, &q.Pattern, &q.IsRegex, &q.CreatedAt, &q.Position); err != nil {
			return running, nil, err
		}
		queued = append(queued, q)
	}
	return running, queued, nil
}

// AcquireConn exposes a dedicated connection for the worker to run a query on,
// so its backend pid is stable for cancellation.
func (d *DB) AcquireConn(ctx context.Context) (*pgxpool.Conn, error) {
	return d.Pool.Acquire(ctx)
}
