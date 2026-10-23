package api

import (
	"path/filepath"
	"testing"
)

func TestContinuousDecode(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "hrdx.sock")
	got := make(chan Request, 1)
	server := NewServer(socket, func(request Request) {
		got <- request
		request.Reply <- Reply{Data: map[string]any{"ok": true}}
	}, nil)
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Close)

	result := call(t, socket, `{"id":"1","method":"continuous.submit","params":{"profile":"p","conversation":"c","content":"hi","request_id":"r1","policy":"steer"}}`)
	if result.Error != nil {
		t.Fatalf("error = %+v", result.Error)
	}
	request := <-got
	payload, ok := request.Payload.(ContinuousSubmit)
	if !ok || payload.RequestID != "r1" || payload.Policy != "steer" || payload.Conversation != "c" {
		t.Fatalf("payload = %#v", request.Payload)
	}

	call(t, socket, `{"id":"2","method":"continuous.hosts"}`)
	if request := <-got; request.Payload != nil {
		t.Fatalf("hosts payload = %#v, want nil", request.Payload)
	}

	result = call(t, socket, `{"id":"3","method":"continuous.decide","params":{"approval":7}}`)
	if result.Error == nil || result.Error.Code != CodeInvalidParams {
		t.Fatalf("malformed params result = %+v", result)
	}

	result = call(t, socket, `{"id":"4","method":"continuous.nope"}`)
	if result.Error == nil || result.Error.Code != CodeUnknownMethod {
		t.Fatalf("unknown continuous method result = %+v", result)
	}
}

func TestEveryContinuousMethodHasTypedPayload(t *testing.T) {
	for method, newParams := range continuousParams {
		if !IsContinuousMethod(method) {
			t.Fatalf("%s not recognised", method)
		}
		if method == MethodContinuousHosts {
			continue
		}
		if derefParams(newParams()) == nil {
			t.Fatalf("%s params do not dereference to a payload", method)
		}
	}
	if IsContinuousMethod("pane.create") {
		t.Fatal("ordinary method classified as continuous")
	}
}
