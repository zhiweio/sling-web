// events.go is the Server-Sent Events hub. One broker fans out typed events
// (execution snapshots, log lines, job/conn changes, notifications) to all
// connected browsers; clients filter by run_id on their side.
package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/flarco/g"
)

// Event is one SSE payload.
type Event struct {
	Type    string `json:"type"` // execution|log|job|connection|notification|ping
	RunID   string `json:"run_id,omitempty"`
	Payload any    `json:"payload,omitempty"`
}

// subscriber is one connected browser.
type subscriber struct {
	ch     chan Event
	closed chan struct{}
	once   sync.Once
}

// Broker fans events out to subscribers.
type Broker struct {
	mu   sync.RWMutex
	subs map[*subscriber]struct{}
}

func NewBroker() *Broker {
	return &Broker{subs: map[*subscriber]struct{}{}}
}

func (b *Broker) subscribe() *subscriber {
	s := &subscriber{ch: make(chan Event, 512), closed: make(chan struct{})}
	b.mu.Lock()
	b.subs[s] = struct{}{}
	b.mu.Unlock()
	return s
}

func (b *Broker) unsubscribe(s *subscriber) {
	s.once.Do(func() { close(s.closed) })
	b.mu.Lock()
	delete(b.subs, s)
	b.mu.Unlock()
}

// Publish sends an event to all subscribers. Slow consumers drop events
// rather than blocking the engine: their UI falls back to polling.
func (b *Broker) Publish(ev Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for s := range b.subs {
		select {
		case s.ch <- ev:
		default:
		}
	}
}

// ServeHTTP implements GET /api/events as a text/event-stream.
func (b *Broker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	s := b.subscribe()
	defer b.unsubscribe(s)

	// initial event tells the client the stream is live
	fmt.Fprint(w, "event: ready\ndata: {}\n\n")
	fl.Flush()

	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-s.closed:
			return
		case ev := <-s.ch:
			payload, err := json.Marshal(ev)
			if err != nil {
				g.Debug("could not marshal event: %s", err)
				continue
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, payload)
			fl.Flush()
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		}
	}
}
