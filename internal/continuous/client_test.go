package continuous_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"testing"

	"github.com/patriceckhart/hrdx/internal/continuous"
	"github.com/patriceckhart/hrdx/internal/continuous/continuoustest"
)

func TestClientCallAndHostError(t *testing.T) {
	host := continuoustest.New()
	host.On("conversation.create", func(params json.RawMessage) (any, string, string) {
		var p struct {
			Workspace string `json:"workspace"`
		}
		_ = json.Unmarshal(params, &p)
		return map[string]any{"id": "conv-" + p.Workspace, "revision": 3}, "", ""
	})
	host.On("conversation.rename", func(json.RawMessage) (any, string, string) {
		return nil, "conflict", "stale revision"
	})
	client, err := continuous.DialWith(context.Background(), host.Dial, "fake", "")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	conv, err := continuous.Host{Client: client}.OpenRoot(context.Background(), "w1", nil)
	if err != nil || conv.ID != "conv-w1" || conv.Revision != 3 {
		t.Fatalf("OpenRoot = %+v, %v", conv, err)
	}
	_, err = continuous.Host{Client: client}.Rename(context.Background(), "conv-w1", 1, "x")
	if continuous.Code(err) != "conflict" {
		t.Fatalf("rename error code = %q (%v), want conflict", continuous.Code(err), err)
	}
}

func TestClientHelloRejected(t *testing.T) {
	host := continuoustest.New()
	host.On("hello", func(json.RawMessage) (any, string, string) { return nil, "unauthorized", "invalid token" })
	if _, err := continuous.DialWith(context.Background(), host.Dial, "fake", "synthetic-token-value"); continuous.Code(err) != "unauthorized" {
		t.Fatalf("dial error = %v, want unauthorized", err)
	}
}

func TestClientPendingCallFailsWhenConnectionCloses(t *testing.T) {
	client, server := net.Pipe()
	c := continuous.NewClient(client)
	done := make(chan error, 1)
	go func() {
		_, err := c.Call(context.Background(), "runtime.status", nil)
		done <- err
	}()
	// Read the request, then drop the connection without answering.
	if _, err := bufio.NewReader(server).ReadBytes('\n'); err != nil {
		t.Fatal(err)
	}
	server.Close()
	if err := <-done; err == nil {
		t.Fatal("call succeeded after the connection closed")
	}
	<-c.Done()
}

func TestClientCancelledCallReturns(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()
	c := continuous.NewClient(client)
	defer c.Close()
	go func() { _, _ = bufio.NewReader(server).ReadBytes('\n') }()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Call(ctx, "runtime.status", nil); err == nil {
		t.Fatal("cancelled call returned no error")
	}
}

func TestPoolReusesAndRedials(t *testing.T) {
	host := continuoustest.New()
	dials := 0
	pool := continuous.NewPool(func(ctx context.Context, address string) (net.Conn, error) {
		dials++
		return host.Dial(ctx, address)
	})
	defer pool.Close()
	profile := continuous.Profile{ID: "p", Address: "/tmp/x.sock"}
	first, err := pool.Get(context.Background(), profile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Get(context.Background(), profile); err != nil || dials != 1 {
		t.Fatalf("second Get dialed again: dials=%d err=%v", dials, err)
	}
	first.Close()
	<-first.Done()
	if _, err := pool.Get(context.Background(), profile); err != nil || dials != 2 {
		t.Fatalf("Get after close: dials=%d err=%v", dials, err)
	}
}

func TestExecutionStates(t *testing.T) {
	type snap = continuous.Snapshot
	cases := []struct {
		name    string
		snap    snap
		blocked []string
		want    string
	}{
		{"ready", snap{}, nil, continuous.StateReady},
		{"running", snap{Run: &continuous.Run{ID: "r", Phase: "tools"}}, nil, continuous.StateRunning},
		{"recovery", snap{Run: &continuous.Run{ID: "r", Phase: "request"}}, []string{"r"}, continuous.StateRecovery},
		{"approval outranks running", snap{Run: &continuous.Run{ID: "r", Phase: "tools"}, Approvals: []continuous.Approval{{ID: "a"}}}, nil, continuous.StateApproval},
		{"queued", snap{Queue: []continuous.Submission{{State: "queued"}}}, nil, continuous.StateQueued},
		{"failed", snap{Run: &continuous.Run{Phase: "done", Outcome: "failed"}}, nil, continuous.StateFailed},
		{"held queue is failed", snap{Conversation: continuous.Conversation{QueueHeld: true}, Queue: []continuous.Submission{{State: "queued"}}}, nil, continuous.StateFailed},
		{"aborted", snap{Run: &continuous.Run{Phase: "done", Outcome: "aborted"}}, nil, continuous.StateAborted},
		{"completed", snap{Run: &continuous.Run{Phase: "done", Outcome: "completed"}}, nil, continuous.StateReady},
	}
	for _, tc := range cases {
		if got := tc.snap.Execution(tc.blocked); got != tc.want {
			t.Errorf("%s: Execution = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestRemoteProfilesRequireTLSAndToken(t *testing.T) {
	if !continuous.IsRemoteAddress("example.com:7000") || continuous.IsRemoteAddress("/tmp/a:b.sock") || continuous.IsRemoteAddress("./host.sock") {
		t.Fatal("IsRemoteAddress misclassifies addresses")
	}
	if !continuous.IsLoopbackAddress("127.0.0.1:7000") || !continuous.IsLoopbackAddress("localhost:1") || continuous.IsLoopbackAddress("example.com:7000") {
		t.Fatal("IsLoopbackAddress misclassifies addresses")
	}
	plain := continuous.Profile{ID: "r", Address: "example.com:7000"}
	if plain.Validate() == nil {
		t.Fatal("remote profile without TLS accepted")
	}
	if _, err := continuous.DialerFor(plain); err == nil {
		t.Fatal("plaintext dialer built for a remote host")
	}
	if (continuous.Profile{ID: "l", Address: "127.0.0.1:7000"}).Validate() != nil {
		t.Fatal("loopback TCP profile rejected")
	}
	managed := continuous.Profile{ID: "m", Address: "example.com:7000", Managed: true, Store: "/s", CWD: "/c", TLSCA: "/ca", TokenFile: "/t"}
	if managed.Validate() == nil {
		t.Fatal("remote managed profile accepted")
	}
	badCA := continuous.Profile{ID: "r", Address: "example.com:7000", TLSCA: "/nonexistent/ca.pem", TokenFile: "/t"}
	if _, err := continuous.DialerFor(badCA); err == nil {
		t.Fatal("missing CA bundle accepted")
	}
}

func TestConfigurePreservesUnknownFields(t *testing.T) {
	host := continuoustest.New()
	host.On("conversation.snapshot", func(json.RawMessage) (any, string, string) {
		return map[string]any{"conversation": map[string]any{"id": "c", "revision": 7,
			"config": map[string]any{"provider": "a", "model": "m1", "instructions": "keep", "tools": []string{"read"}}}}, "", ""
	})
	host.On("conversation.configure", func(json.RawMessage) (any, string, string) {
		return map[string]any{"id": "c"}, "", ""
	})
	client, err := continuous.DialWith(context.Background(), host.Dial, "fake", "")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if _, err := (continuous.Host{Client: client}).Configure(context.Background(), "c", continuous.AgentConfig{Model: "m2"}); err != nil {
		t.Fatal(err)
	}
	var sent struct {
		ExpectedRevision uint64         `json:"expected_revision"`
		Config           map[string]any `json:"config"`
	}
	_ = json.Unmarshal(host.Requests("conversation.configure")[0].Params, &sent)
	if sent.ExpectedRevision != 7 || sent.Config["model"] != "m2" || sent.Config["provider"] != "a" || sent.Config["instructions"] != "keep" || sent.Config["tools"] == nil {
		t.Fatalf("configure sent %+v", sent)
	}
}

func TestOversizedFrameEndsConnection(t *testing.T) {
	client, server := net.Pipe()
	c := continuous.NewClient(client)
	go func() {
		reader := bufio.NewReader(server)
		_, _ = reader.ReadBytes('\n')
		chunk := make([]byte, 1<<20)
		for i := range chunk {
			chunk[i] = 'a'
		}
		for i := 0; i < 10; i++ {
			if _, err := server.Write(chunk); err != nil {
				return
			}
		}
	}()
	if _, err := c.Call(context.Background(), "runtime.status", nil); err == nil {
		t.Fatal("oversized frame accepted")
	}
	server.Close()
}
