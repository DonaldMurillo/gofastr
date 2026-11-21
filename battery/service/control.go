package service

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/internal/filelock"
)

const (
	controlDeadline   = 5 * time.Second
	maxControlRequest = 4 << 10
	maxControlReply   = 64 << 10
	maxSocketPath     = 103
)

type controlRequest struct {
	Op string `json:"op"`
}

type controlResponse struct {
	OK        bool   `json:"ok"`
	Error     string `json:"error,omitempty"`
	PID       int    `json:"pid,omitempty"`
	ProcStart uint64 `json:"proc_start,omitempty"`
	Addr      string `json:"addr,omitempty"`
	Version   string `json:"version,omitempty"`
	Dev       bool   `json:"dev,omitempty"`
	Started   string `json:"started,omitempty"`
	Token     string `json:"token,omitempty"`
}

var acceptRetryDelay = 100 * time.Millisecond

var closeWait = 10 * time.Second

var mcpStopWait = 2 * time.Second

var errAlreadyRunning = errors.New("another instance is already running")

func checkSocketPath(path string) error {
	if len(path) > maxSocketPath {
		return fmt.Errorf("service: control socket path %q is %d bytes, over the %d-byte limit; set %s to a shorter absolute directory", path, len(path), maxSocketPath, dataDirEnv)
	}
	return nil
}

func lockInstance(dir string) (release func(), err error) {
	f, err := os.OpenFile(filepath.Join(dir, "instance.lock"), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("service: open instance lock: %w", err)
	}
	held, err := filelock.TryLock(f)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("service: take instance lock: %w", err)
	}
	if !held {
		f.Close()
		return nil, errAlreadyRunning
	}
	return func() { f.Close() }, nil
}

func listenControl(path string) (net.Listener, error) {
	if err := checkSocketPath(path); err != nil {
		return nil, err
	}
	if c, err := net.DialTimeout("unix", path, time.Second); err == nil {
		c.Close()
		return nil, errAlreadyRunning
	}
	if fi, err := os.Lstat(path); err == nil {
		if fi.IsDir() {
			return nil, fmt.Errorf("service: control socket path %s is a directory", path)
		}
		if err := os.Remove(path); err != nil {
			return nil, fmt.Errorf("service: remove stale control socket: %w", err)
		}
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("service: listen on control socket: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil && currentOS != "windows" {
		ln.Close()
		return nil, fmt.Errorf("service: restrict control socket: %w", err)
	}
	return ln, nil
}

var peerCheck = peerIsSelf

type controlServer struct {
	ln     net.Listener
	handle func(op string) controlResponse
	log    *slog.Logger
	mcp    func(ctx context.Context, in io.Reader, out io.Writer)

	mu        sync.Mutex
	closed    bool
	wg        sync.WaitGroup
	sessions  map[net.Conn]struct{}
	mcpWG     sync.WaitGroup
	mcpEnded  bool
	mcpCtx    context.Context
	cancelMCP context.CancelFunc
}

func (s *controlServer) serve() {
	for {
		c, err := s.ln.Accept()
		if errors.Is(err, net.ErrClosed) {
			return
		}
		if err != nil {
			s.log.Warn("service: control channel accept failed; retrying", "error", err.Error())
			time.Sleep(acceptRetryDelay)
			continue
		}
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			c.Close()
			return
		}
		s.wg.Add(1)
		s.mu.Unlock()
		go func() {
			defer s.wg.Done()
			s.serveConn(c)
		}()
	}
}

func (s *controlServer) endMCP(deadline time.Time) {
	s.mu.Lock()
	s.mcpEnded = true
	if s.cancelMCP != nil {
		s.cancelMCP()
	}
	for c := range s.sessions {
		c.Close()
	}
	s.mu.Unlock()
	waitBounded(&s.mcpWG, deadline, s.log, "an MCP session")
}

func waitBounded(wg *sync.WaitGroup, deadline time.Time, log *slog.Logger, what string) {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	t := time.NewTimer(time.Until(deadline))
	defer t.Stop()
	select {
	case <-done:
	case <-t.C:
		if log != nil {
			log.Warn("service: control channel closed with " + what + " still running")
		}
	}
}

// Close stops accepting, ends open MCP sessions, and waits up to
// closeWait in all for in-flight requests.
func (s *controlServer) Close() {
	s.closeUntil(time.Now().Add(closeWait))
}

func (s *controlServer) closeUntil(deadline time.Time) {
	s.endMCP(deadline)
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.ln.Close()
	waitBounded(&s.wg, deadline, s.log, "a request")
}

func (s *controlServer) serveConn(c net.Conn) {
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(controlDeadline))
	uc, ok := c.(*net.UnixConn)
	if !ok {
		return
	}
	if err := peerCheck(uc); err != nil {
		s.log.Warn("service: refused a control connection from another OS user", "reason", err.Error())
		return
	}
	br := bufio.NewReaderSize(c, maxControlRequest)
	line, err := br.ReadSlice('\n')
	if err != nil {
		writeControl(c, controlResponse{Error: "request must be one JSON line under 4 KiB"})
		return
	}
	var req controlRequest
	if err := handler.UnmarshalStrict(line, &req); err != nil {
		writeControl(c, controlResponse{Error: "malformed request"})
		return
	}
	if req.Op == "mcp" {
		s.serveMCP(c, br)
		return
	}
	writeControl(c, s.dispatch(req.Op))
}

const maxMCPSessions = 16

func (s *controlServer) serveMCP(c net.Conn, in *bufio.Reader) {
	if s.mcp == nil {
		writeControl(c, controlResponse{Error: "this daemon serves no MCP"})
		return
	}
	s.mu.Lock()
	switch {
	case s.closed || s.mcpEnded:
		s.mu.Unlock()
		writeControl(c, controlResponse{Error: "the daemon is stopping"})
		return
	case len(s.sessions) >= maxMCPSessions:
		s.mu.Unlock()
		writeControl(c, controlResponse{Error: fmt.Sprintf("too many MCP sessions (limit %d)", maxMCPSessions)})
		return
	}
	if s.sessions == nil {
		s.sessions = map[net.Conn]struct{}{}
		s.mcpCtx, s.cancelMCP = context.WithCancel(context.Background())
	}
	s.sessions[c] = struct{}{}
	s.mcpWG.Add(1)
	ctx := s.mcpCtx
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.sessions, c)
		s.mu.Unlock()
		s.mcpWG.Done()
	}()

	_ = c.SetDeadline(time.Time{})
	writeControl(c, controlResponse{OK: true})
	defer func() {
		if v := recover(); v != nil {
			s.log.Error("service: MCP session panicked", "panic_type", fmt.Sprintf("%T", v))
		}
	}()
	s.mcp(ctx, in, c)
}

func (s *controlServer) dispatch(op string) (resp controlResponse) {
	defer func() {
		if v := recover(); v != nil {
			s.log.Error("service: control handler panicked", "op", op, "panic_type", fmt.Sprintf("%T", v))
			resp = controlResponse{Error: "internal error"}
		}
	}()
	return s.handle(op)
}

func writeControl(w io.Writer, resp controlResponse) {
	b, err := json.Marshal(resp)
	if err != nil {
		return
	}
	_, _ = w.Write(append(b, '\n'))
}

func callControl(path, op string) (controlResponse, error) {
	c, _, resp, err := openControl(path, op)
	if c != nil {
		c.Close()
	}
	return resp, err
}

func openControl(path, op string) (net.Conn, *bufio.Reader, controlResponse, error) {
	c, err := net.DialTimeout("unix", path, 2*time.Second)
	if err != nil {
		return nil, nil, controlResponse{}, errNotRunning
	}
	resp, br, err := exchangeControl(c, op)
	if err != nil {
		c.Close()
		return nil, nil, resp, err
	}
	return c, br, resp, nil
}

func exchangeControl(c net.Conn, op string) (controlResponse, *bufio.Reader, error) {
	_ = c.SetDeadline(time.Now().Add(controlDeadline))
	b, err := json.Marshal(controlRequest{Op: op})
	if err != nil {
		return controlResponse{}, nil, err
	}
	if _, err := c.Write(append(b, '\n')); err != nil {
		return controlResponse{}, nil, fmt.Errorf("service: control request: %w", err)
	}
	br := bufio.NewReaderSize(c, maxControlReply)
	line, err := br.ReadSlice('\n')
	if err != nil {
		return controlResponse{}, nil, fmt.Errorf("service: control reply: %w", err)
	}
	var resp controlResponse
	if err := handler.UnmarshalStrict(line, &resp); err != nil {
		return controlResponse{}, nil, fmt.Errorf("service: malformed control reply: %w", err)
	}
	if !resp.OK {
		return resp, nil, fmt.Errorf("service: daemon refused %q: %s", op, resp.Error)
	}
	return resp, br, nil
}

var errNotRunning = errors.New("not running")
