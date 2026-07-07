package db

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	_ "embed"
)

//go:embed schema.sql
var schemaSQL string

type DB struct {
	Pool *pgxpool.Pool
}

func Open(ctx context.Context, dsn string) (*DB, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("pgxpool new: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}
	if _, err := pool.Exec(ctx, schemaSQL); err != nil {
		pool.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &DB{Pool: pool}, nil
}

// ResultTable returns the per-job table name for a task id.
// taskID is a uuid; hyphens are stripped to form a safe identifier.
func ResultTable(taskID string) string {
	return "result_" + strings.ReplaceAll(taskID, "-", "")
}

// ResetStuckJobs cleans up jobs left in 'running' state by a crashed previous run.
func (d *DB) ResetStuckJobs(ctx context.Context) {
	rows, err := d.Pool.Query(ctx, `SELECT id FROM query_jobs WHERE status = 'running'`)
	if err != nil {
		log.Printf("reset stuck jobs: %v", err)
		return
	}
	var ids []string
	for rows.Next() {
		var id string
		_ = rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		_, _ = d.Pool.Exec(ctx, fmt.Sprintf(`DROP TABLE IF EXISTS %s`, ResultTable(id)))
		_, _ = d.Pool.Exec(ctx, `UPDATE query_jobs SET status='failed', error='interrupted by server restart', pid=NULL WHERE id=$1`, id)
	}
	if len(ids) > 0 {
		log.Printf("reset %d stuck running job(s)", len(ids))
	}
}
func (d *DB) StartReaper(ctx context.Context, ttlDays int) {
	interval := time.Hour
	if ttlDays <= 0 {
		return
	}
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				d.reapExpired(ctx, ttlDays)
			}
		}
	}()
}

func (d *DB) reapExpired(ctx context.Context, ttlDays int) {
	cutoff := time.Now().AddDate(0, 0, -ttlDays)
	rows, err := d.Pool.Query(ctx, `
        SELECT id FROM query_jobs
        WHERE status IN ('completed','failed','cancelled')
          AND finished_at IS NOT NULL
          AND finished_at < $1`, cutoff)
	if err != nil {
		log.Printf("reaper query: %v", err)
		return
	}
	var ids []string
	for rows.Next() {
		var id string
		_ = rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		_, _ = d.Pool.Exec(ctx, fmt.Sprintf(`DROP TABLE IF EXISTS %s`, ResultTable(id)))
		_, err := d.Pool.Exec(ctx, `DELETE FROM query_jobs WHERE id = $1`, id)
		if err != nil {
			log.Printf("reaper delete %s: %v", id, err)
		}
	}
	if len(ids) > 0 {
		log.Printf("reaper: expired %d job(s)", len(ids))
	}
}
