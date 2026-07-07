package ingest

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"

	"fuckpassword/internal/db"
)

// ErrBusy is returned when an upload is rejected because one is already in progress.
var ErrBusy = errors.New("upload in progress")

// Service handles serialized uploads: streams a request body to a staging file,
// then deduplicates it into the Dataset. One upload at a time, app-instance-wide.
type Service struct {
	db           *db.DB
	partPath     string
	batch        int
	maxLineBytes int
	mu           sync.Mutex
	busy         bool
}

func New(database *db.DB, uploadDir string, batch, maxLineBytes int) *Service {
	_ = os.MkdirAll(uploadDir, 0o755)
	return &Service{
		db:           database,
		partPath:     filepath.Join(uploadDir, "upload.part"),
		batch:        batch,
		maxLineBytes: maxLineBytes,
	}
}

func (s *Service) IsBusy() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.busy
}

// SweepOrphans removes any staging file left by a crashed previous run.
func (s *Service) SweepOrphans() {
	_ = os.Remove(s.partPath)
}

// Upload streams body into the Dataset, returning the count of newly inserted
// unique records and the count of lines dropped for exceeding the byte cap.
// It rejects with ErrBusy if an upload is already running.
func (s *Service) Upload(ctx context.Context, body io.Reader) (int64, int64, error) {
	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		return 0, 0, ErrBusy
	}
	s.busy = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.busy = false
		s.mu.Unlock()
	}()

	f, err := os.Create(s.partPath)
	if err != nil {
		return 0, 0, err
	}
	if _, err := io.Copy(f, body); err != nil {
		f.Close()
		_ = os.Remove(s.partPath)
		return 0, 0, err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(s.partPath)
		return 0, 0, err
	}

	inserted, skipped, err := s.db.IngestFile(ctx, s.partPath, s.batch, s.maxLineBytes)
	_ = os.Remove(s.partPath)
	return inserted, skipped, err
}
