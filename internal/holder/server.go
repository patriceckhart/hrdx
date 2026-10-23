package holder

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aymanbagabas/go-pty"
)

// resolveCommand finds command on $PATH before handing it to go-pty. On
// Windows, go-pty's Cmd.Start resolves a bare command name against the
// child's working directory instead of $PATH when Dir is set, so agent
// binaries elsewhere on PATH would otherwise fail to start.
func resolveCommand(command string) string {
	if resolved, err := exec.LookPath(command); err == nil {
		return resolved
	}
	return command
}

// ringCapacity bounds detached output per session. 1 MiB comfortably
// holds a full screen plus the scrollback a vt10x replay can use.
const ringCapacity = 1 << 20

// session is one PTY-owning subprocess inside the holder.
type session struct {
	id          int64
	command     string
	cwd         string
	pt          pty.Pty
	ptCloseOnce sync.Once
	cmd         *pty.Cmd
	running     atomic.Bool // written by pump on exit, read by handlers

	mu     sync.Mutex
	buffer *ring // detached output, replayed on attach

	// fgName caches the foreground process lookup.
	fgName      string
	fgCheckedAt time.Time
}

// Server is the holder process: it owns sessions and serves exactly one
// TUI client at a time.
type Server struct {
	socket  string
	version string

	mu       sync.Mutex
	sessions map[int64]*session
	nextID   int64
	client   net.Conn // current attached client, nil when detached
	attached map[int64]bool
	clientMu sync.Mutex

	listener net.Listener
	done     chan struct{}
	stopOnce sync.Once // shutdown may be requested by several clients
}

// NewServer prepares a holder for the given socket path.
func NewServer(socket, version string) *Server {
	return &Server{
		socket:   socket,
		version:  version,
		sessions: map[int64]*session{},
		nextID:   1,
		attached: map[int64]bool{},
		done:     make(chan struct{}),
	}
}

// Run listens and serves until a shutdown request arrives. The socket
// file is removed on exit.
func (s *Server) Run() error {
	_ = os.MkdirAll(filepath.Dir(s.socket), 0o755)
	listener, err := net.Listen("unix", s.socket)
	if err != nil {
		return fmt.Errorf("holder listen: %w", err)
	}
	s.listener = listener
	defer func() {
		listener.Close()
		_ = os.Remove(s.socket)
	}()

	go func() {
		<-s.done
		listener.Close()
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-s.done:
				return nil
			default:
				return err
			}
		}
		s.serveClient(conn)
	}
}

// serveClient handles one TUI connection until it disconnects. A new
// connection replaces the previous one (the old TUI is gone or dying).
func (s *Server) serveClient(conn net.Conn) {
	s.clientMu.Lock()
	if s.client != nil {
		_ = s.client.Close()
	}
	s.client = conn
	s.attached = map[int64]bool{}
	s.clientMu.Unlock()

	defer func() {
		s.clientMu.Lock()
		if s.client == conn {
			s.client = nil
			s.attached = map[int64]bool{}
		}
		s.clientMu.Unlock()
		_ = conn.Close()
	}()

	for {
		f, err := readFrame(conn)
		if err != nil {
			return
		}
		switch f.Type {
		case TIn:
			s.mu.Lock()
			target := s.sessions[f.ID]
			s.mu.Unlock()
			if target != nil && target.running.Load() {
				_, _ = target.pt.Write(f.Payload)
			}
		case TCtrl:
			var req request
			if err := unmarshal(f.Payload, &req); err != nil {
				continue
			}
			if req.Op == "shutdown" {
				s.reply(conn, response{Req: req.Req})
				s.shutdown()
				return
			}
			s.reply(conn, s.handle(req))
		}
	}
}

func (s *Server) handle(req request) response {
	switch req.Op {
	case "hello":
		if req.Protocol != Protocol {
			return response{Req: req.Req, Err: fmt.Sprintf("protocol mismatch: holder %d, client %d", Protocol, req.Protocol)}
		}
		return response{Req: req.Req, Version: s.version, Protocol: Protocol}

	case "start":
		id, err := s.start(req)
		if err != nil {
			return response{Req: req.Req, Err: err.Error()}
		}
		return response{Req: req.Req, Session: id, Running: true}

	case "attach":
		target := s.session(req.Session)
		if target == nil {
			return response{Req: req.Req, Err: fmt.Sprintf("session %d not found", req.Session)}
		}
		if req.Cols > 1 && req.Rows > 1 && target.running.Load() {
			_ = target.pt.Resize(req.Cols, req.Rows)
		}
		// Subscribe before replaying buffered output. Holding the session lock
		// keeps live PTY output from overtaking the replay.
		target.mu.Lock()
		s.clientMu.Lock()
		s.attached[target.id] = true
		replay := target.buffer.Bytes()
		replayed := len(replay) == 0 || s.writeToClientLocked(frame{Type: TOut, ID: target.id, Payload: replay})
		s.clientMu.Unlock()
		if replayed {
			target.buffer = newRing(ringCapacity)
		}
		target.mu.Unlock()
		return response{Req: req.Req, Session: target.id, Running: target.running.Load()}

	case "resize":
		target := s.session(req.Session)
		if target == nil {
			return response{Req: req.Req, Err: fmt.Sprintf("session %d not found", req.Session)}
		}
		if target.running.Load() && req.Cols > 1 && req.Rows > 1 {
			_ = target.pt.Resize(req.Cols, req.Rows)
		}
		return response{Req: req.Req}

	case "kill":
		target := s.session(req.Session)
		if target == nil {
			return response{Req: req.Req, Err: fmt.Sprintf("session %d not found", req.Session)}
		}
		s.kill(target)
		return response{Req: req.Req}

	case "fg":
		target := s.session(req.Session)
		if target == nil {
			return response{Req: req.Req, Err: fmt.Sprintf("session %d not found", req.Session)}
		}
		return response{Req: req.Req, Name: target.foreground()}

	case "list":
		s.mu.Lock()
		defer s.mu.Unlock()
		list := make([]SessionInfo, 0, len(s.sessions))
		for _, current := range s.sessions {
			list = append(list, SessionInfo{
				ID: current.id, Command: current.command, CWD: current.cwd, Running: current.running.Load(),
			})
		}
		return response{Req: req.Req, Sessions: list}
	}
	return response{Req: req.Req, Err: "unknown op " + req.Op}
}

// start launches a subprocess on a fresh PTY.
func (s *Server) start(req request) (int64, error) {
	cols, rows := req.Cols, req.Rows
	if cols < 2 {
		cols = 80
	}
	if rows < 2 {
		rows = 24
	}
	ptmx, err := pty.New()
	if err != nil {
		return 0, fmt.Errorf("open pty: %w", err)
	}
	if err := ptmx.Resize(cols, rows); err != nil {
		ptmx.Close()
		return 0, fmt.Errorf("resize pty: %w", err)
	}

	cmd := ptmx.Command(resolveCommand(req.Command), req.Args...)
	cmd.Dir = req.CWD
	cmd.Env = req.Env
	if err := cmd.Start(); err != nil {
		ptmx.Close()
		return 0, fmt.Errorf("start %s: %w", req.Command, err)
	}

	s.mu.Lock()
	id := s.nextID
	s.nextID++
	target := &session{
		id:      id,
		command: req.Command,
		cwd:     req.CWD,
		pt:      ptmx,
		cmd:     cmd,
		buffer:  newRing(ringCapacity),
	}
	target.running.Store(true)
	s.sessions[id] = target
	s.mu.Unlock()

	go s.pump(target)
	return id, nil
}

// pump reads one session's PTY forever, forwarding output to the
// attached client or buffering it while detached.
//
// On Unix the blocking Read below returns EOF on its own once the child
// exits and the kernel drains the pty's buffer. ConPTY on Windows has no
// such guarantee, so a side goroutine waits for the process and closes
// the pty to unblock Read there too; the brief delay first gives any
// last ConPTY output a chance to arrive. On Unix this races harmlessly
// behind the natural EOF, which always wins first in practice.
func (s *Server) pump(target *session) {
	waited := make(chan struct{})
	go func() {
		_ = target.cmd.Wait()
		close(waited)
		time.Sleep(150 * time.Millisecond)
		target.closePt()
	}()

	buffer := make([]byte, 32*1024)
	for {
		n, err := target.pt.Read(buffer)
		if n > 0 {
			payload := make([]byte, n)
			copy(payload, buffer[:n])
			target.mu.Lock()
			if !s.sendSessionOutput(target.id, payload) {
				target.buffer.Write(payload)
			}
			target.mu.Unlock()
		}
		if err != nil {
			break
		}
	}
	<-waited
	target.running.Store(false)
	s.sendToClient(frame{Type: TEvt, Payload: marshal(event{Event: "exited", Session: target.id})})
}

// sendSessionOutput writes output only after the current client has
// subscribed to that session. Until then, pump retains it for replay.
func (s *Server) sendSessionOutput(id int64, payload []byte) bool {
	s.clientMu.Lock()
	defer s.clientMu.Unlock()
	if !s.attached[id] {
		return false
	}
	return s.writeToClientLocked(frame{Type: TOut, ID: id, Payload: payload})
}

// sendToClient writes a non-session frame to the current client.
func (s *Server) sendToClient(f frame) bool {
	s.clientMu.Lock()
	defer s.clientMu.Unlock()
	return s.writeToClientLocked(f)
}

// writeToClientLocked writes while clientMu is held.
func (s *Server) writeToClientLocked(f frame) bool {
	if s.client == nil {
		return false
	}
	if err := writeFrame(s.client, f); err != nil {
		_ = s.client.Close()
		s.client = nil
		s.attached = map[int64]bool{}
		return false
	}
	return true
}

func (s *Server) reply(conn net.Conn, resp response) {
	s.clientMu.Lock()
	defer s.clientMu.Unlock()
	_ = writeFrame(conn, frame{Type: TResp, Payload: marshal(resp)})
}

func (s *Server) session(id int64) *session {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessions[id]
}

// closePt closes the session's PTY exactly once. The exit-detection
// goroutine in pump and an explicit kill can both race to close it
// (Windows' ConPTY especially: closing an already-closed handle while a
// read is in flight can crash rather than error out cleanly), so every
// close goes through this guard.
func (t *session) closePt() {
	t.ptCloseOnce.Do(func() {
		_ = t.pt.Close()
	})
}

func (s *Server) kill(target *session) {
	if target.running.Load() && target.cmd.Process != nil {
		_ = target.cmd.Process.Kill()
	}
	target.closePt()
	s.mu.Lock()
	delete(s.sessions, target.id)
	s.mu.Unlock()
}

// shutdown kills every session and stops the server.
func (s *Server) shutdown() {
	s.mu.Lock()
	all := make([]*session, 0, len(s.sessions))
	for _, current := range s.sessions {
		all = append(all, current)
	}
	s.mu.Unlock()
	for _, current := range all {
		s.kill(current)
	}
	s.stopOnce.Do(func() { close(s.done) })
}

// foreground resolves the session's foreground process name, cached.
func (t *session) foreground() string {
	if !t.running.Load() {
		return ""
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if time.Since(t.fgCheckedAt) < 2*time.Second {
		return t.fgName
	}
	t.fgCheckedAt = time.Now()
	rootPID := 0
	if t.cmd.Process != nil {
		rootPID = t.cmd.Process.Pid
	}
	name := foregroundName(t.pt, rootPID)
	t.fgName = name
	return name
}
