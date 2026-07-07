package queue

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"fuckpassword/internal/db"
)

// Worker runs a single loop that claims the oldest queued Query Job and executes
// it under a per-query statement timeout. At most one job runs at a time.
type Worker struct {
	db     *db.DB
	poll   time.Duration
	timeout time.Duration
}

func New(database *db.DB, timeout time.Duration) *Worker {
	return &Worker{db: database, poll: 500 * time.Millisecond, timeout: timeout}
}

func (w *Worker) Run(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		job, err := w.db.ClaimNextJob(ctx)
		if err != nil {
			log.Printf("worker claim: %v", err)
			w.sleep(ctx)
			continue
		}
		if job == nil {
			w.sleep(ctx)
			continue
		}
		w.execute(ctx, job)
	}
}

func (w *Worker) sleep(ctx context.Context) {
	t := time.NewTimer(w.poll)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

func (w *Worker) execute(ctx context.Context, job *db.Job) {
	conn, err := w.db.AcquireConn(ctx)
	if err != nil {
		_ = w.db.FailJob(ctx, job.ID, "acquire connection: "+err.Error())
		return
	}
	defer conn.Release()

	var pid int32
	if err := conn.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
		_ = w.db.FailJob(ctx, job.ID, "get backend pid: "+err.Error())
		return
	}
	_ = w.db.SetJobPID(ctx, job.ID, pid)

	tbl := db.ResultTable(job.ID)
	pred, arg := buildPredicate(job.IsRegex, job.Pattern)
	createSQL := fmt.Sprintf("CREATE TABLE %s (record_id bigint)", tbl)
	insertSQL := fmt.Sprintf("INSERT INTO %s (record_id) SELECT id FROM records WHERE %s", tbl, pred)
	timeoutMS := int64(w.timeout / time.Millisecond)

	tx, err := conn.Begin(ctx)
	if err != nil {
		_ = w.db.FailJob(ctx, job.ID, "begin tx: "+err.Error())
		return
	}
	if _, err := tx.Exec(ctx, fmt.Sprintf("SET LOCAL statement_timeout = %d", timeoutMS)); err != nil {
		_ = tx.Rollback(ctx)
		_ = w.db.FailJob(ctx, job.ID, "set timeout: "+err.Error())
		return
	}
	if _, err := tx.Exec(ctx, createSQL); err != nil {
		_ = tx.Rollback(ctx)
		_ = w.db.FailJob(ctx, job.ID, "create result table: "+err.Error())
		return
	}
	if _, err := tx.Exec(ctx, insertSQL, arg); err != nil {
		_ = tx.Rollback(ctx)
		// Distinguish user-cancel from timeout/other failure by re-reading status.
		if j, _ := w.db.GetJob(ctx, job.ID); j != nil && j.Status == "cancelled" {
			_ = w.db.DropResultTable(ctx, job.ID)
			return
		}
		_ = w.db.FailJob(ctx, job.ID, friendlyError(err))
		return
	}
	if err := tx.Commit(ctx); err != nil {
		_ = w.db.FailJob(ctx, job.ID, "commit: "+err.Error())
		return
	}

	count, err := w.db.CountResults(ctx, job.ID)
	if err != nil {
		_ = w.db.FailJob(ctx, job.ID, "count results: "+err.Error())
		return
	}
	if err := w.db.CompleteJob(ctx, job.ID, count); err != nil {
		// Job was cancelled between commit and completion; drop the now-orphan result.
		_ = w.db.DropResultTable(ctx, job.ID)
	}
}

// buildPredicate returns the WHERE clause and its single bind argument.
func buildPredicate(isRegex bool, pattern string) (string, any) {
	if isRegex {
		return "text ~ $1", pattern
	}
	return "text ILIKE $1 ESCAPE '\\'", "%" + escapeLike(pattern) + "%"
}

func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

func friendlyError(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "57014" {
		return "Query exceeded the timeout — the pattern is too expensive or could not use the index."
	}
	return err.Error()
}
