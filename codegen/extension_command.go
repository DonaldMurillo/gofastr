package codegen

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Limits on what a command extension may hand back. The response is a JSON
// document describing generated files, so these are orders of magnitude above
// any real generator and exist only so a broken or hostile extension cannot
// drive the generator to OOM by writing forever.
const (
	maxExtensionStdout = 16 << 20 // 16 MiB of JSON response
	maxExtensionStderr = 256 << 10
)

// extensionWaitDelay bounds Wait() once the child has exited (or the context
// is done) but its output pipes are still held open — typically by a
// grandchild the extension forked and did not reap. Without it Wait blocks
// forever on a pipe nobody will ever close, and `gofastr generate` hangs with
// no diagnosable cause. See os/exec (*Cmd).WaitDelay.
//
// A var, not a const, so exec_security_test.go can shrink it and assert the
// bound actually fires rather than asserting the field was assigned.
var extensionWaitDelay = 10 * time.Second

type commandExtension struct {
	name    string
	command []string
	stderr  io.Writer
}

// NewCommandExtension creates an Extension backed by an external command.
func NewCommandExtension(name string, command []string, stderrWriter io.Writer) Extension {
	return &commandExtension{name: name, command: command, stderr: stderrWriter}
}

func (e *commandExtension) Name() string { return e.name }

func (e *commandExtension) RunPhase(ctx context.Context, phase string, genCtx *Context, gen GeneratorConfig, ext ExtensionConfig) (ExtensionResponse, error) {
	if len(e.command) == 0 {
		return ExtensionResponse{}, fmt.Errorf("extension command is empty")
	}
	req := ExtensionRequest{
		ProtocolVersion: ProtocolVersion,
		Phase:           phase,
		ProjectDir:      genCtx.ProjectDir,
		Generator:       gen,
		Extension:       ext,
		Source:          genCtx.Inputs[generatorInputKey(gen, 0)],
		Metadata:        genCtx.Metadata,
		Files:           genCtx.Files.All(),
	}
	body, err := json.Marshal(req)
	if err != nil {
		return ExtensionResponse{}, err
	}
	command := e.command[0]
	cmd := exec.CommandContext(ctx, command, e.command[1:]...)
	cmd.Dir = genCtx.ProjectDir
	cmd.Env = extensionEnv()
	cmd.WaitDelay = extensionWaitDelay
	cmd.Stdin = bytes.NewReader(body)
	stdout := &cappedBuffer{limit: maxExtensionStdout}
	stderr := &cappedBuffer{limit: maxExtensionStderr}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	runErr := cmd.Run()
	e.replayStderr(stderr)
	if runErr != nil {
		return ExtensionResponse{}, runErr
	}
	if stdout.truncated {
		return ExtensionResponse{}, fmt.Errorf("extension %q response is too large (exceeded the %d byte limit)", e.name, stdout.limit)
	}
	if len(bytes.TrimSpace(stdout.Bytes())) == 0 {
		return ExtensionResponse{}, nil
	}
	var res ExtensionResponse
	dec := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&res); err != nil {
		return ExtensionResponse{}, fmt.Errorf("decode extension response: %w", err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return ExtensionResponse{}, fmt.Errorf("decode extension response: trailing JSON content")
		}
		return ExtensionResponse{}, fmt.Errorf("decode extension response: trailing JSON content: %w", err)
	}
	if res.ProtocolVersion != 0 && res.ProtocolVersion != ProtocolVersion {
		return ExtensionResponse{}, fmt.Errorf("extension response protocol_version %d is not supported", res.ProtocolVersion)
	}
	return res, nil
}

// replayStderr forwards the child's stderr to the operator with the bytes that
// drive a terminal removed.
func (e *commandExtension) replayStderr(stderr *cappedBuffer) {
	if e.stderr == nil || stderr.Len() == 0 {
		return
	}
	_, _ = e.stderr.Write(scrubTerminalBytes(stderr.Bytes()))
	if stderr.truncated {
		_, _ = fmt.Fprintf(e.stderr, "\n[gofastr] extension %q stderr truncated at %d bytes\n", e.name, stderr.limit)
	}
}

// scrubTerminalBytes strips the control bytes that let a subprocess *drive*
// the operator's terminal rather than merely write to it: ESC (which opens the
// CSI/OSC sequences that rewrite the window title, clear the scrollback, or
// reposition the cursor over output the operator already read), BEL, CR (which
// returns to column zero so the next write overpaints the line just printed),
// the rest of the C0 range, and DEL. LF and TAB survive — they are layout, not
// control.
//
// This is the terminal member of the same family as
// core/handler/respond.go sanitizeHeaderValue (HTTP header values),
// core/middleware/logging.go safeLogMethod (log attributes) and
// core/stream/sse.go scrubSSEDataLines (SSE field bodies). Each strips a
// different set because each protocol frames differently, so they are
// deliberately separate functions rather than one shared one — SSE must keep
// LF to preserve framing, a header value must keep neither CR nor LF, and a
// terminal must keep LF but lose CR.
func scrubTerminalBytes(b []byte) []byte {
	clean := true
	for _, c := range b {
		if terminalCtrlByte(c) {
			clean = false
			break
		}
	}
	if clean { // fast path: the overwhelmingly common case is ordinary output
		return b
	}
	out := make([]byte, 0, len(b))
	for _, c := range b {
		if terminalCtrlByte(c) {
			continue
		}
		out = append(out, c)
	}
	return out
}

func terminalCtrlByte(c byte) bool {
	if c == '\n' || c == '\t' {
		return false
	}
	return c < 0x20 || c == 0x7f
}

// cappedBuffer accumulates at most limit bytes and silently discards the rest,
// recording that it did. Discarding beats erroring from Write: cmd.Run would
// then kill the child mid-stream and report the write error instead of the
// oversize response, which is a worse message for the same condition.
type cappedBuffer struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if room := c.limit - c.buf.Len(); room > 0 {
		if len(p) <= room {
			return c.buf.Write(p)
		}
		if _, err := c.buf.Write(p[:room]); err != nil {
			return 0, err
		}
	}
	c.truncated = true
	return len(p), nil // absorbed, not an error — see the type comment
}

func (c *cappedBuffer) Bytes() []byte { return c.buf.Bytes() }
func (c *cappedBuffer) Len() int      { return c.buf.Len() }

// extensionEnvAllowlist is what a build-time tool needs in order to *run*:
// where to find binaries, a home and temp directory, locale, and the Go
// toolchain's own configuration for the common case of an extension written in
// Go. Everything the extension needs to do its *job* already arrives on stdin
// (the ExtensionRequest carries the source, the metadata and the whole
// FileSet), and anything project-specific belongs under the extension's
// `config:` key in gofastr.codegen.yml — which is also delivered on stdin.
var extensionEnvAllowlist = map[string]bool{
	"PATH": true, "HOME": true, "TMPDIR": true, "TMP": true, "TEMP": true,
	"USER": true, "LOGNAME": true, "SHELL": true, "LANG": true, "LC_ALL": true,
	"LC_CTYPE": true, "TERM": true, "XDG_CACHE_HOME": true, "XDG_CONFIG_HOME": true,
	// Go toolchain — an extension is most often a Go program.
	"GOROOT": true, "GOPATH": true, "GOBIN": true, "GOCACHE": true,
	"GOMODCACHE": true, "GOTMPDIR": true, "GOFLAGS": true, "GOPROXY": true,
	"GOPRIVATE": true, "GONOPROXY": true, "GONOSUMDB": true, "GOSUMDB": true,
	"GONOSUMCHECK": true, "GOOS": true, "GOARCH": true, "GOARM": true,
	"GOAMD64": true, "GOEXPERIMENT": true, "GOTOOLCHAIN": true, "GOWORK": true,
	"GOENV": true, "GODEBUG": true, "CGO_ENABLED": true, "CC": true, "CXX": true,
}

// extensionEnvAllowlistWindows are additionally required for a process to
// start at all on Windows.
var extensionEnvAllowlistWindows = map[string]bool{
	"SYSTEMROOT": true, "SYSTEMDRIVE": true, "WINDIR": true, "PATHEXT": true,
	"COMSPEC": true, "USERPROFILE": true, "APPDATA": true, "LOCALAPPDATA": true,
	"PROCESSOR_ARCHITECTURE": true, "NUMBER_OF_PROCESSORS": true,
}

// extensionEnv builds the child environment from an allowlist instead of
// inheriting the parent's.
//
// A command extension is an arbitrary binary named by whichever
// gofastr.codegen.yml is in the project — which config_security_test.go's
// threat model already treats as potentially hostile (a cloned repo, a
// dependency vendored into the tree, a teammate's branch); that is why running
// one from a discovered config needs an opt-in at all. Handing that binary the
// developer's whole environment hands it GOFASTR_SECRET (the session signing
// key — forging sessions for the deployed app), DATABASE_URL, cloud
// credentials and CI tokens, none of which the extension protocol has any use
// for. "I ran your generator" must not mean "I gave you my signing key".
func extensionEnv() []string {
	allowed := func(key string) bool {
		if extensionEnvAllowlist[key] {
			return true
		}
		return runtime.GOOS == "windows" && extensionEnvAllowlistWindows[key]
	}
	environ := os.Environ()
	out := make([]string, 0, len(environ))
	for _, kv := range environ {
		key, _, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if allowed(key) {
			out = append(out, kv)
		}
	}
	return out
}
