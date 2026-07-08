package logstream

import (
	"sync"
	"time"
)

// Event is a structured log entry sent to browser clients over SSE.
type Event struct {
	ID      int64          `json:"id"`
	Time    time.Time      `json:"time"`
	Source  string         `json:"source"`
	Level   string         `json:"level"`
	Message string         `json:"message"`
	Fields  map[string]any `json:"fields,omitempty"`
}

type Hub struct {
	mu          sync.Mutex
	nextID      int64
	maxHistory  int
	history     []Event
	subscribers map[chan Event]struct{}
}

func New(maxHistory int) *Hub {
	if maxHistory < 1 {
		maxHistory = 1
	}
	return &Hub{
		maxHistory:  maxHistory,
		subscribers: make(map[chan Event]struct{}),
	}
}

func (h *Hub) Publish(source, level, message string, fields map[string]any) Event {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.nextID++
	ev := Event{
		ID:      h.nextID,
		Time:    time.Now(),
		Source:  source,
		Level:   level,
		Message: message,
		Fields:  fields,
	}
	h.history = append(h.history, ev)
	if len(h.history) > h.maxHistory {
		h.history = h.history[len(h.history)-h.maxHistory:]
	}
	for ch := range h.subscribers {
		select {
		case ch <- ev:
		default:
		}
	}
	return ev
}

func (h *Hub) Recent() []Event {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]Event, len(h.history))
	copy(out, h.history)
	return out
}

func (h *Hub) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 64)

	h.mu.Lock()
	h.subscribers[ch] = struct{}{}
	h.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subscribers, ch)
			close(ch)
			h.mu.Unlock()
		})
	}
	return ch, cancel
}
