package db

import (
	"bufio"
	"context"
	"os"
	"strings"

	"github.com/jackc/pgx/v5"
)

// lineSource streams non-empty lines from a reader as single-column rows for pgx CopyFrom.
// It stops after yielding `max` rows, so a single underlying reader can be batched.
type lineSource struct {
	r    *bufio.Reader
	max  int
	n    int
	line string
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
			if line != "" {
				s.line = line
				s.n++
				return true
			}
		}
		if err != nil {
			return false // io.EOF (or any read error) ends the source
		}
	}
}

func (s *lineSource) Values() ([]any, error) { return []any{s.line}, nil }
func (s *lineSource) Err() error              { return nil }

// IngestFile streams the file at path into the Dataset in batches, deduplicating
// against existing records. Returns the count of newly inserted unique records.
func (d *DB) IngestFile(ctx context.Context, path string, batchSize int) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	br := bufio.NewReaderSize(f, 1<<20)
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `CREATE TEMP TABLE ingest_batch (text text) ON COMMIT DROP`); err != nil {
		return 0, err
	}

	var total int64
	for {
		src := &lineSource{r: br, max: batchSize}
		copied, err := tx.CopyFrom(ctx, pgx.Identifier{"ingest_batch"}, []string{"text"}, src)
		if err != nil {
			return total, err
		}
		if copied == 0 {
			break // EOF
		}
		tag, err := tx.Exec(ctx, `INSERT INTO records (text) SELECT text FROM ingest_batch ON CONFLICT (text) DO NOTHING`)
		if err != nil {
			return total, err
		}
		total += tag.RowsAffected()
		if _, err := tx.Exec(ctx, `TRUNCATE ingest_batch`); err != nil {
			return total, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return total, err
	}
	return total, nil
}
