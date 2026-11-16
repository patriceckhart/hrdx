package bridge

import (
	"bytes"
	"encoding/base64"
	"sync"
	"testing"
)

type recorder struct {
	mu     sync.Mutex
	events []string
	output []OutputEvent
	resets []ResetEvent
}

func (r *recorder) emit(event string, payload any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
	switch value := payload.(type) {
	case OutputEvent:
		r.output = append(r.output, value)
	case ResetEvent:
		r.resets = append(r.resets, value)
	}
}

func decode(t *testing.T, value string) []byte {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestRingKeepsNewest(t *testing.T) {
	r := newRing(8)
	r.Write([]byte("abc"))
	if string(r.Bytes()) != "abc" {
		t.Fatal(string(r.Bytes()))
	}
	r.Write([]byte("defgh"))
	if string(r.Bytes()) != "abcdefgh" {
		t.Fatal(string(r.Bytes()))
	}
	r.Write([]byte("ij"))
	if string(r.Bytes()) != "cdefghij" {
		t.Fatal(string(r.Bytes()))
	}
	r.Write([]byte("0123456789"))
	if string(r.Bytes()) != "23456789" {
		t.Fatal(string(r.Bytes()))
	}
}

func TestFlushBatchesWithMonotonicSeq(t *testing.T) {
	rec := &recorder{}
	hub := NewHub(rec.emit)
	hub.Write(1, []byte("a"))
	hub.Write(1, []byte("b"))
	hub.Write(2, []byte("x"))
	hub.Flush()
	hub.Flush() // nothing pending: no events
	hub.Write(1, []byte("c"))
	hub.Flush()
	if len(rec.output) != 3 {
		t.Fatalf("events = %+v", rec.output)
	}
	var pane1 []OutputEvent
	for _, event := range rec.output {
		if event.PaneID == 1 {
			pane1 = append(pane1, event)
		}
	}
	if len(pane1) != 2 || pane1[0].Seq != 1 || pane1[1].Seq != 2 ||
		string(decode(t, pane1[0].Data)) != "ab" || string(decode(t, pane1[1].Data)) != "c" {
		t.Fatalf("pane 1 = %+v", pane1)
	}
}

func TestSnapshotContinuesSeq(t *testing.T) {
	rec := &recorder{}
	hub := NewHub(rec.emit)
	hub.Write(1, []byte("hello "))
	hub.Flush()
	hub.Write(1, []byte("world"))
	snap := hub.Snapshot(1)
	if snap.Seq != 2 || string(decode(t, snap.Replay)) != "hello world" {
		t.Fatalf("snapshot = %+v", snap)
	}
	hub.Flush()
	if len(rec.output) != 1 {
		t.Fatal("pending output folded into the snapshot was emitted again")
	}
	hub.Write(1, []byte("!"))
	hub.Flush()
	if last := rec.output[len(rec.output)-1]; last.Seq != 3 {
		t.Fatalf("seq after snapshot = %d", last.Seq)
	}
}

func TestOverflowEmitsResetAndKeepsRing(t *testing.T) {
	rec := &recorder{}
	hub := NewHub(rec.emit)
	chunk := bytes.Repeat([]byte("z"), 1<<20)
	for range 5 {
		hub.Write(7, chunk)
	}
	hub.Flush()
	if len(rec.resets) != 1 || len(rec.output) != 0 || rec.resets[0].PaneID != 7 {
		t.Fatalf("events = %v", rec.events)
	}
	snap := hub.Snapshot(7)
	if len(decode(t, snap.Replay)) != ReplayCapacity {
		t.Fatalf("replay = %d bytes", len(decode(t, snap.Replay)))
	}
	hub.Write(7, []byte("after"))
	hub.Flush()
	if len(rec.output) != 1 || rec.output[0].Seq <= snap.Seq {
		t.Fatalf("output after reset = %+v snap %d", rec.output, snap.Seq)
	}
}

func TestDropForgetsPane(t *testing.T) {
	rec := &recorder{}
	hub := NewHub(rec.emit)
	hub.Write(3, []byte("x"))
	hub.Drop(3)
	hub.Flush()
	if len(rec.events) != 0 {
		t.Fatal("dropped pane emitted")
	}
}

func TestConcurrentWritesAreOrdered(t *testing.T) {
	rec := &recorder{}
	hub := NewHub(rec.emit)
	done := make(chan struct{})
	go func() {
		for i := range 2000 {
			hub.Write(1, []byte{byte('a' + i%26)})
		}
		close(done)
	}()
	for {
		hub.Flush()
		select {
		case <-done:
			hub.Flush()
			var all []byte
			last := uint64(0)
			for _, event := range rec.output {
				if event.Seq != last+1 {
					t.Fatalf("seq gap %d -> %d", last, event.Seq)
				}
				last = event.Seq
				all = append(all, decode(t, event.Data)...)
			}
			if len(all) != 2000 || all[27] != 'b' {
				t.Fatalf("bytes = %d", len(all))
			}
			return
		default:
		}
	}
}
