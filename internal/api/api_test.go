package api

import (
	"bufio"
	"encoding/json"
	"net"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// echoServer answers every forwarded request with its method name, or an
// error for pane.close (standing in for a model-level failure).
func echoServer(t *testing.T, events *Broadcaster) (string, *Server) {
	t.Helper()
	socket := filepath.Join(t.TempDir(), "hrdx.sock")
	server := NewServer(socket, func(request Request) {
		if request.Method == "pane.close" {
			request.Reply <- Reply{Err: "nope", Code: CodeNotFound}
			return
		}
		request.Reply <- Reply{Data: map[string]any{"method": request.Method}}
	}, events)
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Close)
	return socket, server
}

type wireResult struct {
	ID     string          `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *wireError      `json:"error"`
	Event  string          `json:"event"`
	Data   json.RawMessage `json:"data"`
}

// call sends one NDJSON line and reads one response line.
func call(t *testing.T, socket, line string) wireResult {
	t.Helper()
	conn, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte(line + "\n")); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		t.Fatalf("no response for %s", line)
	}
	var parsed wireResult
	if err := json.Unmarshal(scanner.Bytes(), &parsed); err != nil {
		t.Fatalf("bad response %q: %v", scanner.Text(), err)
	}
	return parsed
}

func TestServerPing(t *testing.T) {
	socket, _ := echoServer(t, nil)
	response := call(t, socket, `{"id": "r1", "method": "ping"}`)
	if response.ID != "r1" || response.Error != nil {
		t.Fatalf("ping = %+v", response)
	}
	var result struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(response.Result, &result)
	if result.Type != "pong" {
		t.Fatalf("ping result = %s", response.Result)
	}
}

func TestServerRoutesMethods(t *testing.T) {
	socket, _ := echoServer(t, nil)
	for _, method := range []string{
		"status", "workspace.create", "workspace.close", "workspace.move", "group.list",
		"pane.create", "pane.send_text", "pane.read", "menu.register",
	} {
		line := `{"id": "r", "method": "` + method + `", "params": {}}`
		response := call(t, socket, line)
		if response.Error != nil {
			t.Fatalf("%s error: %+v", method, response.Error)
		}
		var result struct {
			Method string `json:"method"`
		}
		_ = json.Unmarshal(response.Result, &result)
		if result.Method != method {
			t.Fatalf("%s routed to %q", method, result.Method)
		}
	}
}

func TestServerErrors(t *testing.T) {
	socket, _ := echoServer(t, nil)

	response := call(t, socket, `{"id": "r1", "method": "explode"}`)
	if response.Error == nil || response.Error.Code != CodeUnknownMethod {
		t.Fatalf("unknown method = %+v", response.Error)
	}

	response = call(t, socket, `{"id": "r2", "method": "pane.close", "params": {"pane_id": 1}}`)
	if response.Error == nil || response.Error.Code != CodeNotFound || response.Error.Message != "nope" {
		t.Fatalf("model error = %+v", response.Error)
	}

	response = call(t, socket, `{"id": "r3", "method": "pane.send_text", "params": {"pane_id": "x"}}`)
	if response.Error == nil || response.Error.Code != CodeInvalidParams {
		t.Fatalf("bad params = %+v", response.Error)
	}

	response = call(t, socket, `{not json`)
	if response.Error == nil || response.Error.Code != CodeInvalidParams {
		t.Fatalf("bad json = %+v", response.Error)
	}
}

func TestServerWait(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "hrdx.sock")
	var busy atomic.Bool
	busy.Store(true)
	server := NewServer(socket, func(request Request) {
		if request.Method != "pane.busy" {
			request.Reply <- Reply{Err: "unexpected " + request.Method}
			return
		}
		request.Reply <- Reply{Data: busy.Load()}
	}, nil)
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	// Already-busy pane satisfies until=busy immediately.
	response := call(t, socket, `{"id": "w1", "method": "pane.wait", "params": {"pane_id": 1, "until": "busy"}}`)
	if response.Error != nil {
		t.Fatalf("wait busy = %+v", response.Error)
	}

	// Flip to idle shortly; the until=idle wait must resolve.
	go func() {
		time.Sleep(300 * time.Millisecond)
		busy.Store(false)
	}()
	response = call(t, socket, `{"id": "w2", "method": "pane.wait", "params": {"pane_id": 1, "until": "idle", "timeout_ms": 5000}}`)
	if response.Error != nil {
		t.Fatalf("wait idle = %+v", response.Error)
	}

	// Bad until value.
	response = call(t, socket, `{"id": "w3", "method": "pane.wait", "params": {"pane_id": 1, "until": "gone"}}`)
	if response.Error == nil || response.Error.Code != CodeInvalidParams {
		t.Fatalf("bad until = %+v", response.Error)
	}
}

func TestServerWaitTimeout(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "hrdx.sock")
	server := NewServer(socket, func(request Request) {
		request.Reply <- Reply{Data: true} // always busy
	}, nil)
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	response := call(t, socket, `{"id": "w", "method": "pane.wait", "params": {"pane_id": 1, "until": "idle", "timeout_ms": 300}}`)
	if response.Error == nil || response.Error.Code != CodeTimeout {
		t.Fatalf("timeout = %+v", response.Error)
	}
}

func TestServerSubscription(t *testing.T) {
	events := NewBroadcaster()
	socket, _ := echoServer(t, events)

	conn, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte(`{"id": "s1", "method": "events.subscribe"}` + "\n")); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	scanner := bufio.NewScanner(conn)

	// Acknowledgement first.
	if !scanner.Scan() {
		t.Fatal("no subscribe ack")
	}
	var ack wireResult
	_ = json.Unmarshal(scanner.Bytes(), &ack)
	if ack.ID != "s1" || ack.Error != nil {
		t.Fatalf("ack = %+v", ack)
	}

	// A published event arrives as a pushed line. Give the server a
	// moment to register the subscriber before publishing.
	deadline := time.Now().Add(2 * time.Second)
	go func() {
		for time.Now().Before(deadline) {
			events.Publish(Event{Event: EventPaneBusyChanged, Data: PaneEvent{Pane: 7, Busy: true}})
			time.Sleep(50 * time.Millisecond)
		}
	}()
	if !scanner.Scan() {
		t.Fatal("no pushed event")
	}
	var pushed wireResult
	_ = json.Unmarshal(scanner.Bytes(), &pushed)
	if pushed.Event != EventPaneBusyChanged {
		t.Fatalf("pushed = %+v", pushed)
	}
	var data PaneEvent
	_ = json.Unmarshal(pushed.Data, &data)
	if data.Pane != 7 || !data.Busy {
		t.Fatalf("event data = %+v", data)
	}
}

func TestBroadcasterDropsSlowSubscribers(t *testing.T) {
	events := NewBroadcaster()
	id, channel := events.Subscribe()
	defer events.Unsubscribe(id)
	// Overflow the buffer; Publish must never block.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 1000; i++ {
			events.Publish(Event{Event: "x"})
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish blocked on a slow subscriber")
	}
	if len(channel) == 0 {
		t.Fatal("subscriber should have buffered events")
	}
}

func TestServerReplacesStaleSocket(t *testing.T) {
	dir := t.TempDir()
	socket := filepath.Join(dir, "hrdx.sock")

	// A dead socket file left behind by a crash.
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	listener.Close()
	if _, err := net.Dial("unix", socket); err == nil {
		t.Skip("socket still answers, cannot simulate staleness")
	}

	server := NewServer(socket, func(request Request) {
		request.Reply <- Reply{Data: "ok"}
	}, nil)
	if err := server.Start(); err != nil {
		t.Fatalf("Start over stale socket: %v", err)
	}
	defer server.Close()
}
