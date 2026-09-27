package api

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"time"
)

// requestTimeout bounds how long a handler waits for the TUI to answer.
const requestTimeout = 5 * time.Second

// maxLine caps a single request line (1 MiB is far beyond any sane call).
const maxLine = 1 << 20

// wireRequest is one incoming NDJSON line.
type wireRequest struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

// wireError is the error object of a failed response.
type wireError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// wireResponse is one outgoing NDJSON line: a reply or a pushed event.
type wireResponse struct {
	ID     string     `json:"id,omitempty"`
	Result any        `json:"result,omitempty"`
	Error  *wireError `json:"error,omitempty"`
	Event  string     `json:"event,omitempty"`
	Data   any        `json:"data,omitempty"`
}

// Server accepts NDJSON connections on a unix socket and forwards
// requests into the TUI update loop.
type Server struct {
	socket    string
	send      func(Request)
	events    *Broadcaster
	listener  net.Listener
	waitLimit time.Duration // max wait for pane.wait, overridable in tests
}

// NewServer prepares a server; send forwards a request into the TUI's
// update loop (typically program.Send). events may be nil when
// subscriptions are not needed.
func NewServer(socket string, send func(Request), events *Broadcaster) *Server {
	return &Server{socket: socket, send: send, events: events, waitLimit: 10 * time.Minute}
}

// Start listens on the unix socket and serves in the background. A stale
// socket file from a crashed instance is replaced when nothing answers on
// it; a live socket is an error so two instances never fight.
func (s *Server) Start() error {
	listener, err := net.Listen("unix", s.socket)
	if err != nil {
		conn, dialErr := net.DialTimeout("unix", s.socket, time.Second)
		if dialErr == nil {
			conn.Close()
			return fmt.Errorf("socket %s is in use by another instance", s.socket)
		}
		if removeErr := os.Remove(s.socket); removeErr != nil {
			return err
		}
		listener, err = net.Listen("unix", s.socket)
		if err != nil {
			return err
		}
	}
	s.listener = listener

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go s.serve(conn)
		}
	}()
	return nil
}

// Close stops the listener and removes the socket file.
func (s *Server) Close() {
	if s.listener != nil {
		_ = s.listener.Close()
	}
	_ = os.Remove(s.socket)
}

// serve handles one connection: read a line, answer a line. A
// subscription switches the connection into push mode.
func (s *Server) serve(conn net.Conn) {
	defer conn.Close()
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 64*1024), maxLine)
	encoder := json.NewEncoder(conn)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var request wireRequest
		if err := json.Unmarshal(line, &request); err != nil {
			_ = encoder.Encode(wireResponse{Error: &wireError{Code: CodeInvalidParams, Message: "invalid json: " + err.Error()}})
			continue
		}
		if request.Method == "events.subscribe" {
			s.serveSubscription(conn, encoder, request)
			return
		}
		_ = encoder.Encode(s.dispatch(request))
	}
}

// dispatch decodes one request's params, round-trips it through the TUI,
// and shapes the response line.
func (s *Server) dispatch(request wireRequest) wireResponse {
	fail := func(code, message string) wireResponse {
		return wireResponse{ID: request.ID, Error: &wireError{Code: code, Message: message}}
	}

	var payload any
	switch request.Method {
	case "ping":
		return wireResponse{ID: request.ID, Result: map[string]string{"type": "pong"}}
	case "status", "plugins.status", "group.list":
		payload = nil
	case "plugins.control":
		var params PluginControl
		if err := decodeParams(request.Params, &params); err != nil {
			return fail(CodeInvalidParams, err.Error())
		}
		payload = params
	case "workspace.create":
		var params WorkspaceCreate
		if err := decodeParams(request.Params, &params); err != nil {
			return fail(CodeInvalidParams, err.Error())
		}
		payload = params
	case "workspace.move":
		var params WorkspaceMove
		if err := decodeParams(request.Params, &params); err != nil {
			return fail(CodeInvalidParams, err.Error())
		}
		payload = params
	case "workspace.close":
		var params WorkspaceRef
		if err := decodeParams(request.Params, &params); err != nil {
			return fail(CodeInvalidParams, err.Error())
		}
		payload = params
	case "pane.create":
		var params PaneCreate
		if err := decodeParams(request.Params, &params); err != nil {
			return fail(CodeInvalidParams, err.Error())
		}
		payload = params
	case "pane.send_text":
		var params PaneSendText
		if err := decodeParams(request.Params, &params); err != nil {
			return fail(CodeInvalidParams, err.Error())
		}
		payload = params
	case "pane.read":
		var params PaneRef
		if err := decodeParams(request.Params, &params); err != nil {
			return fail(CodeInvalidParams, err.Error())
		}
		payload = params
	case "pane.close":
		var params PaneRef
		if err := decodeParams(request.Params, &params); err != nil {
			return fail(CodeInvalidParams, err.Error())
		}
		payload = params
	case "menu.register":
		var params MenuRegister
		if err := decodeParams(request.Params, &params); err != nil {
			return fail(CodeInvalidParams, err.Error())
		}
		payload = params
	case "pane.wait":
		var params AgentWait
		if err := decodeParams(request.Params, &params); err != nil {
			return fail(CodeInvalidParams, err.Error())
		}
		return s.waitForState(request.ID, params)
	default:
		return fail(CodeUnknownMethod, "unknown method "+request.Method)
	}

	answer, ok := s.roundTrip(request.Method, payload, requestTimeout)
	if !ok {
		return fail(CodeTimeout, "hrdx did not answer")
	}
	if answer.Err != "" {
		code := answer.Code
		if code == "" {
			code = CodeError
		}
		return fail(code, answer.Err)
	}
	return wireResponse{ID: request.ID, Result: answer.Data}
}

// roundTrip forwards one request into the TUI and waits for the answer.
func (s *Server) roundTrip(method string, payload any, timeout time.Duration) (Reply, bool) {
	reply := make(chan Reply, 1)
	s.send(Request{Method: method, Payload: payload, Reply: reply})
	select {
	case answer := <-reply:
		return answer, true
	case <-time.After(timeout):
		return Reply{}, false
	}
}

// waitForState polls the pane's busy state through the update loop until
// it matches the requested state or the timeout expires. Polling keeps
// the update loop untouched; at 250ms the latency is far below the
// debounce already applied to finish detection.
func (s *Server) waitForState(id string, params AgentWait) wireResponse {
	fail := func(code, message string) wireResponse {
		return wireResponse{ID: id, Error: &wireError{Code: code, Message: message}}
	}
	wantBusy := false
	switch params.Until {
	case "idle":
		wantBusy = false
	case "busy":
		wantBusy = true
	default:
		return fail(CodeInvalidParams, "until must be idle or busy")
	}
	limit := s.waitLimit
	if params.TimeoutMS > 0 {
		limit = time.Duration(params.TimeoutMS) * time.Millisecond
	}
	deadline := time.Now().Add(limit)

	for {
		answer, ok := s.roundTrip("pane.busy", PaneRef{Pane: params.Pane}, requestTimeout)
		if !ok {
			return fail(CodeTimeout, "hrdx did not answer")
		}
		if answer.Err != "" {
			code := answer.Code
			if code == "" {
				code = CodeError
			}
			return fail(code, answer.Err)
		}
		busy, _ := answer.Data.(bool)
		if busy == wantBusy {
			return wireResponse{ID: id, Result: map[string]any{
				"type": "pane_wait", "pane_id": params.Pane, "state": params.Until,
			}}
		}
		if time.Now().After(deadline) {
			return fail(CodeTimeout, "wait timed out")
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// serveSubscription acknowledges the subscribe request and pushes events
// until the client disconnects.
func (s *Server) serveSubscription(conn net.Conn, encoder *json.Encoder, request wireRequest) {
	if s.events == nil {
		_ = encoder.Encode(wireResponse{ID: request.ID, Error: &wireError{Code: CodeError, Message: "events unavailable"}})
		return
	}
	id, channel := s.events.Subscribe()
	defer s.events.Unsubscribe(id)
	if err := encoder.Encode(wireResponse{ID: request.ID, Result: map[string]string{"type": "subscribed"}}); err != nil {
		return
	}

	// Detect client disconnect: the read side unblocks with an error
	// once the peer goes away.
	done := make(chan struct{})
	go func() {
		defer close(done)
		discard := make([]byte, 256)
		for {
			if _, err := conn.Read(discard); err != nil {
				return
			}
		}
	}()

	for {
		select {
		case event, open := <-channel:
			if !open {
				return
			}
			if err := encoder.Encode(wireResponse{Event: event.Event, Data: event.Data}); err != nil {
				return
			}
		case <-done:
			return
		}
	}
}

func decodeParams(raw json.RawMessage, into any) error {
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("invalid params: %s", err)
	}
	return nil
}
