package tasklock

import (
	"context"
	"sync"
	"time"
)

// Snapshot describes the task currently holding the shared upload/query lock.
type Snapshot struct {
	Busy  bool       `json:"busy"`
	Kind  string     `json:"kind,omitempty"`
	ID    string     `json:"id,omitempty"`
	Label string     `json:"label,omitempty"`
	Since *time.Time `json:"since,omitempty"`
}

type holder struct {
	kind  string
	id    string
	label string
	since time.Time
}

// Lock is a process-local mutex for work that must not overlap.
type Lock struct {
	slot chan struct{}
	mu   sync.Mutex
	held *holder
}

func New() *Lock {
	l := &Lock{slot: make(chan struct{}, 1)}
	l.slot <- struct{}{}
	return l
}

func (l *Lock) TryAcquire(kind, id, label string) (func(), bool) {
	select {
	case <-l.slot:
		return l.hold(kind, id, label), true
	default:
		return nil, false
	}
}

func (l *Lock) Acquire(ctx context.Context, kind, id, label string) (func(), error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-l.slot:
		return l.hold(kind, id, label), nil
	}
}

func (l *Lock) hold(kind, id, label string) func() {
	l.mu.Lock()
	l.held = &holder{kind: kind, id: id, label: label, since: time.Now()}
	l.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			l.held = nil
			l.mu.Unlock()
			l.slot <- struct{}{}
		})
	}
}

func (l *Lock) Update(kind, id, label string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.held == nil {
		return
	}
	l.held.kind = kind
	l.held.id = id
	l.held.label = label
}

func (l *Lock) Snapshot() Snapshot {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.held == nil {
		return Snapshot{}
	}
	since := l.held.since
	return Snapshot{
		Busy:  true,
		Kind:  l.held.kind,
		ID:    l.held.id,
		Label: l.held.label,
		Since: &since,
	}
}
