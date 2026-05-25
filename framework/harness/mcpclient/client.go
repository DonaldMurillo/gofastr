// Package mcpclient implements the MCP client (consumer side) the
// harness uses to talk to external MCP servers.
//
// v0.1: stdio transport only (the only one needed for the
// gofastr-introspection and kiln MCP servers shipped with the v0.1
// profile presets). HTTP+SSE / streamable HTTP land in v0.2.
//
// Wire format: JSON-RPC 2.0 (newline-delimited).
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
	"sync"
	"sync/atomic"
)

// Client speaks the MCP wire protocol over stdio against a child
// process. Concurrency-safe.
type Client struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser

	mu         sync.Mutex
	nextID     atomic.Uint64
	pending    map[uint64]chan response
	closed     atomic.Bool

	// initialized is true once the MCP `initialize` handshake completed.
	initialized atomic.Bool

	// Handler hooks for notifications/* from the server (resource
	// updates, etc.). v0.1 doesn't subscribe to any; resourced
	// subscriptions land with mcpserver in v0.2.
}

// Spawn launches the MCP server subprocess and performs the
// `initialize` handshake. If `expectedSHA256` is non-empty, the
// binary is checked against the hash and refused on mismatch.
func Spawn(ctx context.Context, cmd string, args []string, expectedSHA256 string) (*Client, error) {
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

func (c *Client) initialize(ctx context.Context) error {
	params := map[string]any{
		"protocolVersion": "2024-11-05",
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
	// MCP requires an `initialized` notification after the response.
	return c.Notify(ctx, "notifications/initialized", nil)
}

// ToolDescriptor is the tier-1 metadata for one tool.
type ToolDescriptor struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
}

// ListTools requests the server's tools/list. With schemas=false
// (lazy discovery), inputSchema fields are dropped client-side to
// save the per-startup tier-2 cost.
func (c *Client) ListTools(ctx context.Context) ([]ToolDescriptor, error) {
	resp, err := c.Call(ctx, "tools/list", nil)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Tools []ToolDescriptor `json:"tools"`
	}
	if err := json.Unmarshal(resp, &parsed); err != nil {
		return nil, err
	}
	return parsed.Tools, nil
}

// CallTool invokes a tool. Returns the structured result.
func (c *Client) CallTool(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, error) {
	params := map[string]any{
		"name":      name,
		"arguments": args,
	}
	return c.Call(ctx, "tools/call", params)
}

// ---------- JSON-RPC plumbing ----------

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

// Call issues a JSON-RPC request and waits for the response.
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

// Notify sends a notification (no response expected).
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
	for scanner.Scan() {
		var r response
		if err := json.Unmarshal(scanner.Bytes(), &r); err != nil {
			continue
		}
		if r.ID == 0 {
			// Notification — v0.1 doesn't subscribe to any.
			continue
		}
		c.mu.Lock()
		ch := c.pending[r.ID]
		c.mu.Unlock()
		if ch != nil {
			ch <- r
		}
	}
}

func (c *Client) drainStderr() {
	// Per the doc, MCP servers log to stderr; we discard for v0.1
	// (the harness's own logger logs the subprocess output via the
	// MCP supervisor in a later phase).
	_, _ = io.Copy(io.Discard, c.stderr)
}

// Close terminates the child process.
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

// sha256OfBinary hashes the file at path.
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

// SHA256MismatchError is returned when the configured binary hash
// doesn't match the on-disk hash. Maps to the
// ReasonMCPServerSHA256Mismatch wire error code.
type SHA256MismatchError struct {
	Path     string
	Expected string
	Actual   string
}

func (e *SHA256MismatchError) Error() string {
	return fmt.Sprintf("mcpclient: sha256 mismatch on %s: expected %s, got %s", e.Path, e.Expected, e.Actual)
}

// ErrClosed is returned when methods are called after Close.
var ErrClosed = errors.New("mcpclient: closed")
