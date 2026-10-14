package service

import (
	"bufio"
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
)

// The control channel. A Unix socket (AF_UNIX on Windows too) inside
// the 0700 state directory, one JSON request line and one JSON
// response line per connection. Directory permissions are not trusted
// on their own (some systems ignore socket file modes), so every
// accepted connection is checked against the daemon's own OS user
// through the kernel's peer credentials before a byte is read.
//
// Ops: "status" (pid, address, version), "token" (a 60-second
// single-use sign-in token), "stop" (graceful shutdown; the Windows
// stop path, where there is no SIGTERM to send).

const (
	controlDeadline   = 5 * time.Second
	maxControlRequest = 4 << 10
	maxControlReply   = 64 << 10
	// maxSocketPath is the smaller sun_path limit (macOS: 104 bytes
	// including the terminator; Linux and Windows: 108).
	maxSocketPath = 103
)

type controlRequest struct {
	Op string `json:"op"`
}

type controlResponse struct {
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
	PID     int    `json:"pid,omitempty"`
	Addr    string `json:"addr,omitempty"`
	Version string `json:"version,omitempty"`
	Dev     bool   `json:"dev,omitempty"`
	Started string `json:"started,omitempty"`
	Token   string `json:"token,omitempty"`
}

// acceptRetryDelay paces control-channel Accept retries.
var acceptRetryDelay = 100 * time.Millisecond

var errAlreadyRunning = errors.New("another instance is already running")

// checkSocketPath refuses a control socket path over the sun_path
// limit, naming the fix.
func checkSocketPath(path string) error {
	if len(path) > maxSocketPath {
		return fmt.Errorf("service: control socket path %q is %d bytes, over the %d-byte limit; set %s to a shorter absolute directory", path, len(path), maxSocketPath, dataDirEnv)
	}
	return nil
}

// lockInstance takes an exclusive OS lock on the state directory's
// lock file and holds it until release. It is what makes one instance
// per state directory: two processes that both find no live socket
// would otherwise both remove and re-bind it (check, then act), and
// both open the database. The kernel drops the lock when the process
// dies, so a crash leaves nothing to clean up.
func lockInstance(dir string) (release func(), err error) {
	f, err := os.OpenFile(filepath.Join(dir, "instance.lock"), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("service: open instance lock: %w", err)
	}
	held, err := tryLock(f)
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

// listenControl claims the control socket. A live socket means another
// instance is serving (refused); a dead one is a leftover from a crash
// and is removed.
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

// peerCheck is peerIsSelf, a variable so a test can stand in a foreign
// peer, which a unit test cannot produce for real.
var peerCheck = peerIsSelf

// controlServer answers control requests until Close.
type controlServer struct {
	ln     net.Listener
	handle func(op string) controlResponse
	log    *slog.Logger

	// mu orders wg.Add in serve against wg.Wait in Close: an Add that
	// races a Wait on a zero counter is a WaitGroup misuse, and a
	// connection accepted as Close runs would escape the wait.
	mu     sync.Mutex
	closed bool
	wg     sync.WaitGroup
}

// serve returns only once Close has closed the listener. Any other
// Accept error (a full file table, an aborted connection) is retried:
// returning would leave the socket bound but deaf, so every client
// would time out for the rest of the daemon's life.
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

// Close stops accepting and waits for in-flight requests.
func (s *controlServer) Close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.ln.Close()
	s.wg.Wait()
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
	line, err := bufio.NewReader(io.LimitReader(c, maxControlRequest)).ReadBytes('\n')
	if err != nil {
		writeControl(c, controlResponse{Error: "request must be one JSON line under 4 KiB"})
		return
	}
	var req controlRequest
	if err := handler.UnmarshalStrict(line, &req); err != nil {
		writeControl(c, controlResponse{Error: "malformed request"})
		return
	}
	writeControl(c, s.dispatch(req.Op))
}

// dispatch runs the handler under a recover guard: a panic in one
// request must not take the daemon down.
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

// callControl sends one request and returns the reply. The error
// distinguishes "nothing is listening" (errNotRunning) from a refusal.
func callControl(path, op string) (controlResponse, error) {
	c, err := net.DialTimeout("unix", path, 2*time.Second)
	if err != nil {
		return controlResponse{}, errNotRunning
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(controlDeadline))
	b, err := json.Marshal(controlRequest{Op: op})
	if err != nil {
		return controlResponse{}, err
	}
	if _, err := c.Write(append(b, '\n')); err != nil {
		return controlResponse{}, fmt.Errorf("service: control request: %w", err)
	}
	line, err := bufio.NewReader(io.LimitReader(c, maxControlReply)).ReadBytes('\n')
	if err != nil {
		return controlResponse{}, fmt.Errorf("service: control reply: %w", err)
	}
	var resp controlResponse
	if err := handler.UnmarshalStrict(line, &resp); err != nil {
		return controlResponse{}, fmt.Errorf("service: malformed control reply: %w", err)
	}
	if !resp.OK {
		return resp, fmt.Errorf("service: daemon refused %q: %s", op, resp.Error)
	}
	return resp, nil
}

var errNotRunning = errors.New("not running")
