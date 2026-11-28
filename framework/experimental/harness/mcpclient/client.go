package mcpclient

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

// Client speaks MCP (newline-delimited JSON-RPC 2.0) over stdio to a child process; safe for concurrent use.
type Client struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser

	mu          sync.Mutex
	nextID      atomic.Uint64
	pending     map[uint64]chan response
	closed      atomic.Bool
	initialized atomic.Bool
}

// Spawn starts the server with an allowlisted environment, refuses a binary whose hash differs from a non-empty expectedSHA256, and runs the initialize handshake.
func Spawn(ctx context.Context, cmd string, args []string, expectedSHA256 string) (*Client, error) {
	return SpawnWithConfig(ctx, cmd, args, expectedSHA256, SpawnConfig{})
}

// SpawnConfig adds child environment beyond the allowlist: Env entries ("KEY=VALUE") win on a name collision, InheritEnv names host vars to copy.
type SpawnConfig struct {
	Env        []string
	InheritEnv []string
}

// SpawnWithConfig is Spawn with extra child environment from cfg.
func SpawnWithConfig(ctx context.Context, cmd string, args []string, expectedSHA256 string, cfg SpawnConfig) (*Client, error) {
	if expectedSHA256 != "" {
		got, err := sha256OfBinary(cmd)
		if err != nil {
			return nil, fmt.Errorf("mcpclient: hash %s: %w", cmd, err)
		}
		if got != expectedSHA256 {
			return nil, &SHA256MismatchError{Path: cmd, Expected: expectedSHA256, Actual: got}
		}
	}
	c := exec.CommandContext(ctx, cmd, args...)
	c.Env = buildChildEnv(cfg)
	c.WaitDelay = 5 * time.Second
	stdin, err := c.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := c.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := c.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := c.Start(); err != nil {
		return nil, err
	}
	cl := &Client{
		cmd:     c,
		stdin:   stdin,
		stdout:  stdout,
		stderr:  stderr,
		pending: make(map[uint64]chan response),
	}
	go cl.readLoop()
	go cl.drainStderr()
	if err := cl.initialize(ctx); err != nil {
		_ = cl.Close()
		return nil, err
	}
	return cl, nil
}

func defaultEnvAllowlist() []string {
	base := []string{"LANG", "LC_ALL", "LC_CTYPE", "PATH", "HOME", "TMPDIR", "USER", "LOGNAME"}
	if runtime.GOOS == "windows" {
		return []string{"LANG", "LC_ALL", "PATH", "SYSTEMROOT", "COMSPEC", "PATHEXT", "TEMP", "TMP", "USERPROFILE"}
	}
	return base
}

func buildChildEnv(cfg SpawnConfig) []string {
	want := defaultEnvAllowlist()
	want = append(want, cfg.InheritEnv...)
	env := make([]string, 0, len(cfg.Env)+len(want))
	seen := make(map[string]bool, len(cfg.Env)+len(want))
	for _, kv := range cfg.Env {
		name, _, ok := strings.Cut(kv, "=")
		if !ok || name == "" || seen[name] {
			continue
		}
		seen[name] = true
		env = append(env, kv)
	}
	for _, name := range want {
		if seen[name] {
			continue
		}
		seen[name] = true
		if v, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+v)
		}
	}
	return env
}

func (c *Client) initialize(ctx context.Context) error {
	params := map[string]any{
		"protocolVersion": "2025-06-18",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "gofastr-harness",
			"version": "0.1.0",
		},
	}
	if _, err := c.Call(ctx, "initialize", params); err != nil {
		return fmt.Errorf("mcpclient: initialize: %w", err)
	}
	c.initialized.Store(true)
	return c.Notify(ctx, "notifications/initialized", nil)
}

// ToolDescriptor is one entry of a server's tools/list.
type ToolDescriptor struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
}

// ListTools returns the server's tools/list, refusing a result with duplicate or case-folded keys.
func (c *Client) ListTools(ctx context.Context) ([]ToolDescriptor, error) {
	resp, err := c.Call(ctx, "tools/list", nil)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Tools []ToolDescriptor `json:"tools"`
	}
	if err := handler.CheckObjectKeys(resp, handler.FoldJSONKey); err != nil {
		return nil, err
	}
	//gofastr:allow(GOFASTR1407) the CheckObjectKeys walk above already refused duplicate and case-folded keys at every depth; this Unmarshal only decodes a vetted body
	if err := json.Unmarshal(resp, &parsed); err != nil {
		return nil, err
	}
	return parsed.Tools, nil
}

// CallTool sends tools/call for name with args and returns the raw result.
func (c *Client) CallTool(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, error) {
	params := map[string]any{
		"name":      name,
		"arguments": args,
	}
	return c.Call(ctx, "tools/call", params)
}

type request struct {
	JSONRPC string `json:"jsonrpc"`
	ID      uint64 `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      uint64          `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Call sends a JSON-RPC request and waits for its response, ctx, or a scanner error that stops the read loop while the call is pending.
func (c *Client) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if c.closed.Load() {
		return nil, ErrClosed
	}
	id := c.nextID.Add(1)
	respCh := make(chan response, 1)
	c.mu.Lock()
	c.pending[id] = respCh
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	body, err := json.Marshal(request{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		return nil, err
	}
	if err := c.write(body); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-respCh:
		if r.Error != nil {
			return nil, fmt.Errorf("mcpclient: %s: %s", method, r.Error.Message)
		}
		return r.Result, nil
	}
}

// Notify sends a JSON-RPC notification.
func (c *Client) Notify(_ context.Context, method string, params any) error {
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	})
	if err != nil {
		return err
	}
	return c.write(body)
}

func (c *Client) write(line []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, err := c.stdin.Write(append(line, '\n')); err != nil {
		return err
	}
	return nil
}

func (c *Client) readLoop() {
	scanner := bufio.NewScanner(c.stdout)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	defer func() {
		if err := scanner.Err(); err != nil {
			c.failPending(err)
		}
	}()
	for scanner.Scan() {
		var r response
		if err := handler.CheckObjectKeys(scanner.Bytes(), handler.FoldJSONKey); err != nil {
			continue
		}
		//gofastr:allow(GOFASTR1407) the CheckObjectKeys walk above already refused duplicate and case-folded keys at every depth; this Unmarshal only decodes a vetted line
		if err := json.Unmarshal(scanner.Bytes(), &r); err != nil {
			continue
		}
		if r.ID == 0 {
			continue
		}
		c.mu.Lock()
		ch := c.pending[r.ID]
		c.mu.Unlock()
		if ch != nil {
			select {
			case ch <- r:
			default:
			}
		}
	}
}

func (c *Client) failPending(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, ch := range c.pending {
		select {
		case ch <- response{ID: id, Error: &rpcError{Message: "read: " + err.Error()}}:
		default:
		}
	}
}

func (c *Client) drainStderr() {
	_, _ = io.Copy(io.Discard, c.stderr)
}

// Close kills the child process and waits for it; later calls return nil.
func (c *Client) Close() error {
	if !c.closed.CompareAndSwap(false, true) {
		return nil
	}
	_ = c.stdin.Close()
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	return c.cmd.Wait()
}

func sha256OfBinary(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// SHA256MismatchError reports a server binary whose hash is not the configured one.
type SHA256MismatchError struct {
	Path     string
	Expected string
	Actual   string
}

func (e *SHA256MismatchError) Error() string {
	return fmt.Sprintf("mcpclient: sha256 mismatch on %s: expected %s, got %s", e.Path, e.Expected, e.Actual)
}

// ErrClosed is returned by Call after Close.
var ErrClosed = errors.New("mcpclient: closed")
