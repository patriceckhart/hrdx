// Package bridge connects the core to the webview: Wails bound methods,
// output batching, and per-pane replay rings. It imports no Wails package;
// the runtime is injected through Platform so everything here is testable
// without a webview.
package bridge

import (
	"encoding/base64"
	"sync"
	"time"
)

// Limits from the desktop spec.
const (
	ReplayCapacity = 2 << 20 // per-pane replay ring
	BatchLimit     = 4 << 20 // pending batch that triggers pane:reset
	FlushInterval  = 8 * time.Millisecond
)

// ring is a fixed-capacity byte ring keeping the newest data. Same
// semantics as the holder's replay ring.
type ring struct {
	buf  []byte
	size int
	head int // next write position
	full bool
}

func newRing(capacity int) *ring { return &ring{buf: make([]byte, capacity), size: capacity} }

func (r *ring) Write(data []byte) {
	if len(data) >= r.size {
		copy(r.buf, data[len(data)-r.size:])
		r.head = 0
		r.full = true
		return
	}
	n := copy(r.buf[r.head:], data)
	if n < len(data) {
		copy(r.buf, data[n:])
		r.full = true
	}
	next := r.head + len(data)
	if next >= r.size {
		r.full = true
	}
	r.head = next % r.size
}

func (r *ring) Bytes() []byte {
	if !r.full {
		return append([]byte(nil), r.buf[:r.head]...)
	}
	out := make([]byte, 0, r.size)
	out = append(out, r.buf[r.head:]...)
	return append(out, r.buf[:r.head]...)
}

// stream is the output state of one pane.
type stream struct {
	ring    *ring
	pending []byte
	seq     uint64 // seq of the last emitted event
	reset   bool   // overflow: emit pane:reset instead of the batch
}

// OutputEvent is the pane:output payload.
type OutputEvent struct {
	PaneID int    `json:"pane_id"`
	Seq    uint64 `json:"seq"`
	Data   string `json:"data"` // base64
}

// ResetEvent is the pane:reset payload.
type ResetEvent struct {
	PaneID int `json:"pane_id"`
}

// Replay is what a mounting terminal needs to catch up.
type Replay struct {
	Seq    uint64 `json:"seq"`
	Replay string `json:"replay"` // base64
}

// Hub buffers pane output and emits it in batches. Write is called from
// holder and PTY I/O goroutines and never blocks on the webview.
type Hub struct {
	mu      sync.Mutex
	streams map[int]*stream
	emit    func(event string, payload any)
	done    chan struct{}
	once    sync.Once
}

// NewHub returns a hub emitting through emit. Call Run to start flushing.
func NewHub(emit func(event string, payload any)) *Hub {
	return &Hub{streams: map[int]*stream{}, emit: emit, done: make(chan struct{})}
}

func (h *Hub) streamLocked(id int) *stream {
	current := h.streams[id]
	if current == nil {
		current = &stream{ring: newRing(ReplayCapacity)}
		h.streams[id] = current
	}
	return current
}

// Write appends output for a pane.
func (h *Hub) Write(id int, data []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	current := h.streamLocked(id)
	current.ring.Write(data)
	if current.reset {
		return
	}
	if len(current.pending)+len(data) > BatchLimit {
		current.pending = nil
		current.reset = true
		return
	}
	current.pending = append(current.pending, data...)
}

// Drop forgets a closed pane.
func (h *Hub) Drop(id int) {
	h.mu.Lock()
	delete(h.streams, id)
	h.mu.Unlock()
}

// Snapshot returns the replay ring and the seq it is current to. Output
// still pending is folded into the snapshot and the seq advanced, so live
// events with a greater seq continue exactly where the replay ends.
func (h *Hub) Snapshot(id int) Replay {
	h.mu.Lock()
	defer h.mu.Unlock()
	current := h.streamLocked(id)
	if len(current.pending) > 0 || current.reset {
		current.pending = nil
		current.reset = false
		current.seq++
	}
	return Replay{Seq: current.seq, Replay: base64.StdEncoding.EncodeToString(current.ring.Bytes())}
}

// Flush emits every pending batch once. Events are emitted outside the lock.
func (h *Hub) Flush() {
	type outgoing struct {
		event   string
		payload any
	}
	var out []outgoing
	h.mu.Lock()
	for id, current := range h.streams {
		switch {
		case current.reset:
			current.reset = false
			current.seq++
			out = append(out, outgoing{"pane:reset", ResetEvent{PaneID: id}})
		case len(current.pending) > 0:
			current.seq++
			out = append(out, outgoing{"pane:output", OutputEvent{PaneID: id, Seq: current.seq,
				Data: base64.StdEncoding.EncodeToString(current.pending)}})
			current.pending = nil
		}
	}
	h.mu.Unlock()
	for _, event := range out {
		h.emit(event.event, event.payload)
	}
}

// Run flushes every FlushInterval until Stop.
func (h *Hub) Run() {
	ticker := time.NewTicker(FlushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-h.done:
			return
		case <-ticker.C:
			h.Flush()
		}
	}
}

// Stop ends Run.
func (h *Hub) Stop() { h.once.Do(func() { close(h.done) }) }
