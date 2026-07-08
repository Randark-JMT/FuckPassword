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
	"fuckpassword/internal/logstream"
	"fuckpassword/internal/tasklock"
)

// ErrBusy is returned when an upload is rejected because another task is active.
var ErrBusy = errors.New("task in progress")

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
	BytesReceived  int64      `json:"bytes_received"`
	LinesTotal     int64      `json:"lines_total"`
	LinesProcessed int64      `json:"lines_processed"`
	Inserted       int64      `json:"inserted"`
	Skipped        int64      `json:"skipped"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
	Error          string     `json:"error,omitempty"`
}

// Service handles serialized uploads. Each Upload streams its Source File to a
// staging file (Phase A, synchronous), then deduplicates it into the Dataset in
// a background goroutine (Phase B). The shared task lock spans both phases: it
// is acquired at the start of Phase A and released only when Phase B finishes,
// so uploads cannot overlap with either another upload or a running query.
type Service struct {
	db           *db.DB
	tasks        *tasklock.Lock
	logs         *logstream.Hub
	partPath     string
	batch        int
	maxLineBytes int
	mu           sync.Mutex
	busy         bool
	appCtx       context.Context
	progress     Progress
}

func New(database *db.DB, tasks *tasklock.Lock, logs *logstream.Hub, uploadDir string, batch, maxLineBytes int) *Service {
	_ = os.MkdirAll(uploadDir, 0o755)
	return &Service{
		db:           database,
		tasks:        tasks,
		logs:         logs,
		partPath:     filepath.Join(uploadDir, "upload.part"),
		batch:        batch,
		maxLineBytes: maxLineBytes,
		progress:     Progress{Phase: PhaseIdle},
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

// StartUpload acquires the shared task lock, runs Phase A (stream body to
// staging), then spawns a goroutine for Phase B. Returns nil on success
// (handler responds 202; caller polls Snapshot), ErrBusy if another task is
// active, or the Phase A error. On a Phase A error the lock is released and the
// phase is set to error.
func (s *Service) StartUpload(body io.Reader, contentLength int64) error {
	release, ok := s.tasks.TryAcquire("upload", "", "Receiving upload")
	if !ok {
		s.publish("upload", "warn", "Upload rejected because another task is running", nil)
		return ErrBusy
	}

	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		release()
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

	s.publish("upload", "info", "Upload started", map[string]any{"bytes_total": contentLength})

	f, err := os.Create(s.partPath)
	if err != nil {
		s.markFailed(err)
		release()
		return err
	}
	received, lines, err := s.copyUpload(f, body)
	if err != nil {
		f.Close()
		_ = os.Remove(s.partPath)
		s.markFailed(err)
		release()
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(s.partPath)
		s.markFailed(err)
		release()
		return err
	}

	s.mu.Lock()
	s.progress.Phase = PhaseProcessing
	s.progress.BytesReceived = received
	s.progress.LinesTotal = lines
	s.progress.LinesProcessed = 0
	s.mu.Unlock()

	s.tasks.Update("upload", "", "Processing uploaded records")
	s.publish("upload", "info", "Upload received; processing records", map[string]any{
		"bytes_received": received,
		"lines_total":    lines,
	})

	go s.runPhaseB(release)
	return nil
}

func (s *Service) copyUpload(dst *os.File, src io.Reader) (received int64, nonEmptyLines int64, err error) {
	buf := make([]byte, 1<<20)
	lineHasContent := false
	for {
		n, readErr := src.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			written, writeErr := dst.Write(chunk)
			received += int64(written)
			for _, b := range chunk[:written] {
				if b == '\n' {
					if lineHasContent {
						nonEmptyLines++
					}
					lineHasContent = false
				} else if b != '\r' {
					lineHasContent = true
				}
			}
			s.updateUploadReceive(received, nonEmptyLines)
			if writeErr != nil {
				return received, nonEmptyLines, writeErr
			}
			if written != n {
				return received, nonEmptyLines, io.ErrShortWrite
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return received, nonEmptyLines, readErr
		}
	}
	if lineHasContent {
		nonEmptyLines++
	}
	s.updateUploadReceive(received, nonEmptyLines)
	return received, nonEmptyLines, nil
}

func (s *Service) updateUploadReceive(received, lines int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.progress.Phase != PhaseUploading {
		return
	}
	s.progress.BytesReceived = received
	s.progress.LinesTotal = lines
}

func (s *Service) runPhaseB(release func()) {
	defer release()
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
		total := s.progress.LinesTotal
		s.mu.Unlock()
		fields := map[string]any{
			"lines_processed": processed,
			"lines_total":     total,
			"inserted":        inserted,
			"dropped":         skipped,
		}
		if total > 0 {
			fields["percent"] = float64(processed) / float64(total) * 100
		}
		s.publish("upload", "info", "Processing records", fields)
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
		s.publish("upload", "error", "Upload failed", map[string]any{"error": err.Error()})
	} else {
		s.progress.Phase = PhaseDone
		s.progress.Inserted = inserted
		s.progress.Skipped = skipped
		if s.progress.LinesProcessed < s.progress.LinesTotal {
			s.progress.LinesProcessed = s.progress.LinesTotal
		}
		s.publish("upload", "info", "Upload completed", map[string]any{
			"lines_processed": s.progress.LinesProcessed,
			"lines_total":     s.progress.LinesTotal,
			"inserted":        inserted,
			"dropped":         skipped,
			"percent":         100,
		})
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
	s.publish("upload", "error", "Upload failed", map[string]any{"error": err.Error()})
}

func (s *Service) Snapshot() Progress {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.progress
}

func (s *Service) publish(source, level, message string, fields map[string]any) {
	if s.logs == nil {
		return
	}
	s.logs.Publish(source, level, message, fields)
}
