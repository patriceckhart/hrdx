// Package continuoustest provides an in-memory zot continuous host for
// tests. It speaks the newline-delimited JSON host protocol over net.Pipe,
// so tests need no zot binary, store, socket, or network.
package continuoustest

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"
)

// Handler answers one request: data on success, or code and message.
type Handler func(params json.RawMessage) (data any, code, message string)

// Host is a scripted fake host. Methods without a handler fail with
// bad_request, like an unknown method on a real host.
type Host struct {
	mu       sync.Mutex
	handlers map[string]Handler
	requests []Request
	down     bool
}

// Request records one received request.
type Request struct {
	Method string
	Params json.RawMessage
}

func New() *Host { return &Host{handlers: map[string]Handler{}} }

// On sets the handler for method.
func (h *Host) On(method string, handler Handler) {
	h.mu.Lock()
	h.handlers[method] = handler
	h.mu.Unlock()
}

// SetDown makes new dials fail, simulating an unreachable host.
func (h *Host) SetDown(down bool) {
	h.mu.Lock()
	h.down = down
	h.mu.Unlock()
}

// Requests returns the recorded requests for method.
func (h *Host) Requests(method string) []Request {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []Request
	for _, request := range h.requests {
		if request.Method == method {
			out = append(out, request)
		}
	}
	return out
}

// Dial satisfies continuous.Dialer.
func (h *Host) Dial(context.Context, string) (net.Conn, error) {
	h.mu.Lock()
	down := h.down
	h.mu.Unlock()
	if down {
		return nil, fmt.Errorf("connection refused")
	}
	client, server := net.Pipe()
	go h.serve(server)
	return client, nil
}

func (h *Host) serve(conn net.Conn) {
	defer conn.Close()
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for scanner.Scan() {
		var request struct {
			ID     string          `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			return
		}
		h.mu.Lock()
		h.requests = append(h.requests, Request{Method: request.Method, Params: append(json.RawMessage(nil), request.Params...)})
		handler := h.handlers[request.Method]
		h.mu.Unlock()
		response := map[string]any{"id": request.ID, "type": "response", "method": request.Method}
		if handler == nil {
			response["success"], response["error"], response["code"] = false, "unknown method", "bad_request"
		} else if data, code, message := handler(request.Params); message != "" {
			response["success"], response["error"], response["code"] = false, message, code
		} else {
			response["success"], response["data"] = true, data
		}
		line, _ := json.Marshal(response)
		if _, err := conn.Write(append(line, '\n')); err != nil {
			return
		}
	}
}
