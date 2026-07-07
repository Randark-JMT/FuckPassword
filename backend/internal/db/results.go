package db

import (
	"context"
	"fmt"
	"io"
)

// FetchResults returns up to `limit` matched record texts for a job, offset by `offset`.
func (d *DB) FetchResults(ctx context.Context, taskID string, offset, limit int) ([]string, error) {
	tbl := ResultTable(taskID)
	q := fmt.Sprintf(`
        SELECT rec.text
          FROM %s r JOIN records rec ON rec.id = r.record_id
         ORDER BY r.record_id
         LIMIT $1 OFFSET $2`, tbl)
	rows, err := d.Pool.Query(ctx, q, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// CountResults returns the number of matched records materialized for a job.
func (d *DB) CountResults(ctx context.Context, taskID string) (int, error) {
	var n int
	err := d.Pool.QueryRow(ctx, fmt.Sprintf(`SELECT count(*) FROM %s`, ResultTable(taskID))).Scan(&n)
	return n, err
}

// StreamResultsCopy streams the full result set as COPY text format to w (one record per line).
func (d *DB) StreamResultsCopy(ctx context.Context, taskID string, w io.Writer) error {
	conn, err := d.Pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	sql := fmt.Sprintf(`COPY (SELECT rec.text FROM %s r JOIN records rec ON rec.id = r.record_id ORDER BY r.record_id) TO STDOUT`, ResultTable(taskID))
	_, err = conn.Conn().PgConn().CopyTo(ctx, w, sql)
	return err
}

// DropResultTable drops a job's result table if present.
func (d *DB) DropResultTable(ctx context.Context, taskID string) error {
	_, err := d.Pool.Exec(ctx, fmt.Sprintf(`DROP TABLE IF EXISTS %s`, ResultTable(taskID)))
	return err
}
