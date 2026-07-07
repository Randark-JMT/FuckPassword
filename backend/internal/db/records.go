package db

import (
	"bufio"
	"context"
	"os"
	"strings"

	"github.com/jackc/pgx/v5"
)

// lineSource streams non-empty lines from a reader as single-column rows for pgx CopyFrom.
// It stops after yielding `max` rows and skips (and counts) lines longer than `maxBytes`,
// so a single shared reader can be batched and oversized lines never reach the table.
type lineSource struct {
	r        *bufio.Reader
	max      int
	maxBytes int
	n        int
	skipped  int64
	line     string
}

func (s *lineSource) Next() bool {
	if s.max > 0 && s.n >= s.max {
		return false
	}
	for {
		chunk, err := s.r.ReadString('\n')
		if len(chunk) > 0 {
			line := strings.TrimRight(chunk, "\n")
			line = strings.TrimRight(line, "\r")
			if line == "" {
				if err != nil {
					return false
				}
				continue
			}
			if s.maxBytes > 0 && len(line) > s.maxBytes {
				s.skipped++
				if err != nil {
					return false
				}
				continue
			}
			s.line = line
			s.n++
			return true
		}
		if err != nil {
			return false
		}
	}
}

func (s *lineSource) Values() ([]any, error) { return []any{s.line}, nil }
func (s *lineSource) Err() error              { return nil }

// IngestFile streams the file at path into the Dataset in batches, dropping lines
// longer than maxLineBytes and deduplicating against existing records via the
// text_hash unique index. Returns newly-inserted unique records and dropped-line count.
func (d *DB) IngestFile(ctx context.Context, path string, batchSize, maxLineBytes int) (inserted int64, skipped int64, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()

	br := bufio.NewReaderSize(f, 1<<20)
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `CREATE TEMP TABLE ingest_batch (text text) ON COMMIT DROP`); err != nil {
		return 0, 0, err
	}

	for {
		src := &lineSource{r: br, max: batchSize, maxBytes: maxLineBytes}
		copied, err := tx.CopyFrom(ctx, pgx.Identifier{"ingest_batch"}, []string{"text"}, src)
		skipped += src.skipped
		if err != nil {
			return inserted, skipped, err
		}
		if copied == 0 {
			break // EOF
		}
		tag, err := tx.Exec(ctx, `INSERT INTO records (text) SELECT text FROM ingest_batch ON CONFLICT (text_hash) DO NOTHING`)
		if err != nil {
			return inserted, skipped, err
		}
		inserted += tag.RowsAffected()
		if _, err := tx.Exec(ctx, `TRUNCATE ingest_batch`); err != nil {
			return inserted, skipped, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return inserted, skipped, err
	}
	return inserted, skipped, nil
}
