package ingest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"fuckpassword/internal/db"
)

// ErrBusy is returned when an upload is rejected because one is already in progress.
var ErrBusy = errors.New("upload in progress")

const (
	PhaseIdle       = "idle"
	PhaseUploading  = "uploading"
	PhaseProcessing = "processing"
	PhaseDone       = "done"
	PhaseError      = "error"
)

// Progress is a point-in-time snapshot of the current (or most recent) Upload.
type Progress struct {
	Phase          string     `json:"phase"`
	BytesTotal     int64      `json:"bytes_total"`
	LinesProcessed int64      `json:"lines_processed"`
	Inserted       int64      `json:"inserted"`
	Skipped        int64      `json:"skipped"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
	Error          string     `json:"error,omitempty"`
}

// Service handles serialized uploads. Each Upload streams its Source File to a
// staging file (Phase A, synchronous), then deduplicates it into the Dataset in
// a background goroutine (Phase B). The busy lock spans both phases: it is
// acquired at the start of Phase A and released only when Phase B finishes, so
// a second POST during either phase is rejected with ErrBusy.
//
// TODO: if this tool ever serves multiple concurrent users, replace the single
// progress slot with a monotonic generation id so a delayed poll from a previous
// Upload cannot be confused with the current one.
type Service struct {
	db           *db.DB
	partPath     string
	batch        int
	maxLineBytes int
	mu           sync.Mutex
	busy         bool
	appCtx       context.Context
	progress     Progress
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

// Start attaches the app-level lifecycle context used by Phase B goroutines.
// Phase B must not run under the request context, which is canceled the moment
// the handler returns 202. Called once at boot, mirroring worker.Run(ctx).
func (s *Service) Start(ctx context.Context) { s.appCtx = ctx }

func (s *Service) IsBusy() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.busy
}

// SweepOrphans removes any staging file left by a crashed previous run.
func (s *Service) SweepOrphans() {
	_ = os.Remove(s.partPath)
}

// StartUpload acquires the busy lock, runs Phase A (stream body to staging),
// then spawns a goroutine for Phase B. Returns nil on success (handler responds
// 202; caller polls Snapshot), ErrBusy if already running, or the Phase A error.
// On a Phase A error the busy lock is released and phase is set to error.
func (s *Service) StartUpload(body io.Reader, contentLength int64) error {
	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		return ErrBusy
	}
	s.busy = true
	now := time.Now()
	s.progress = Progress{
		Phase:      PhaseUploading,
		BytesTotal: contentLength,
		StartedAt:  &now,
	}
	s.mu.Unlock()

	f, err := os.Create(s.partPath)
	if err != nil {
		s.markFailed(err)
		return err
	}
	if _, err := io.Copy(f, body); err != nil {
		f.Close()
		_ = os.Remove(s.partPath)
		s.markFailed(err)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(s.partPath)
		s.markFailed(err)
		return err
	}

	s.mu.Lock()
	s.progress.Phase = PhaseProcessing
	s.mu.Unlock()

	go s.runPhaseB()
	return nil
}

func (s *Service) runPhaseB() {
	defer func() {
		if r := recover(); r != nil {
			_ = os.Remove(s.partPath)
			s.markFailed(fmt.Errorf("phase b panic: %v", r))
		}
	}()

	onBatch := func(processed, inserted, skipped int64) {
		s.mu.Lock()
		s.progress.LinesProcessed = processed
		s.progress.Inserted = inserted
		s.progress.Skipped = skipped
		s.mu.Unlock()
	}

	inserted, skipped, err := s.db.IngestFile(s.appCtx, s.partPath, s.batch, s.maxLineBytes, onBatch)
	_ = os.Remove(s.partPath)

	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.progress.FinishedAt = &now
	if err != nil {
		s.progress.Phase = PhaseError
		s.progress.Error = err.Error()
	} else {
		s.progress.Phase = PhaseDone
		s.progress.Inserted = inserted
		s.progress.Skipped = skipped
	}
	s.busy = false
}

func (s *Service) markFailed(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.progress.Phase = PhaseError
	s.progress.Error = err.Error()
	s.progress.FinishedAt = &now
	s.busy = false
}

func (s *Service) Snapshot() Progress {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.progress
}
