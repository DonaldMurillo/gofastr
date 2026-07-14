package evalrunner

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type agentRequest struct {
	Config     AgentConfig
	Workspace  string
	OutputPath string
	SchemaPath string
	Images     []string
	Prompt     string
	LogPath    string
	Env        []string
	Judge      bool
}

func runAgent(ctx context.Context, req agentRequest) error {
	switch req.Config.Backend {
	case "codex":
		args := builderArgs(req.Config, req.Workspace, req.OutputPath, req.Config.Model)
		if req.Judge {
			args = judgeArgs(req.Config, req.Workspace, req.OutputPath, req.SchemaPath, req.Config.Model, req.Images)
		}
		return runCodex(ctx, codexInvocation{
			Program: req.Config.Program,
			Args:    args, Env: req.Env, Dir: req.Workspace, Stdin: req.Prompt,
			LogPath: req.LogPath, Timeout: time.Duration(req.Config.TimeoutMinutes) * time.Minute,
		})
	case "omp":
		if req.Judge {
			return fmt.Errorf("omp/%s cannot judge screenshot evidence: its model catalog reports images=no", req.Config.Model)
		}
		args := append([]string{}, req.Config.PrefixArgs...)
		args = append(args,
			"-p", "--cwd", req.Workspace, "--model", req.Config.Model,
			"--no-session", "--approval-mode", "yolo", "--no-title",
			"--no-extensions", "--mode", "json",
		)
		if req.Config.Effort != "" {
			args = append(args, "--thinking", req.Config.Effort)
		}
		args = append(args, req.Prompt)
		if err := runCapturedAgent(ctx, req.Config, args, req.Workspace, req.Env, req.LogPath); err != nil {
			return err
		}
		return extractOMPFinal(req.LogPath, req.OutputPath)
	case "claude":
		args := append([]string{}, req.Config.PrefixArgs...)
		args = append(args,
			"-p", "--model", req.Config.Model,
			"--no-session-persistence", "--output-format", "json",
			"--prompt-suggestions", "false", "--setting-sources", "project",
		)
		if req.Config.Effort != "" {
			args = append(args, "--effort", req.Config.Effort)
		}
		prompt := req.Prompt
		if req.Judge {
			schema, err := os.ReadFile(req.SchemaPath)
			if err != nil {
				return fmt.Errorf("read judge schema: %w", err)
			}
			schema, err = claudeCompatibleSchema(schema)
			if err != nil {
				return fmt.Errorf("adapt judge schema for Claude: %w", err)
			}
			args = append(args, "--permission-mode", "dontAsk", "--allowedTools", "Read", "--json-schema", string(schema))
			prompt += "\n\nRead every evidence image with the Read tool before scoring:\n"
			for _, image := range req.Images {
				prompt += "- " + filepath.Base(image) + "\n"
			}
		} else {
			args = append(args, "--permission-mode", "bypassPermissions", "--tools", "default")
		}
		args = append(args, prompt)
		if err := runCapturedAgent(ctx, req.Config, args, req.Workspace, req.Env, req.LogPath); err != nil {
			return err
		}
		return extractClaudeFinal(req.LogPath, req.OutputPath)
	default:
		return fmt.Errorf("unsupported agent backend %q", req.Config.Backend)
	}
}

// Claude Code validates --json-schema against its bundled schema dialect and
// rejects the otherwise-valid Draft 2020-12 declaration URI before launching a
// model. The judge schema uses only keywords shared by Claude's supported
// dialect, so remove the dialect annotation while preserving every constraint.
func claudeCompatibleSchema(schema []byte) ([]byte, error) {
	var document map[string]json.RawMessage
	if err := json.Unmarshal(schema, &document); err != nil {
		return nil, err
	}
	delete(document, "$schema")
	return json.Marshal(document)
}

func runCapturedAgent(ctx context.Context, cfg AgentConfig, args []string, dir string, env []string, logPath string) error {
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return err
	}
	stdout, err := os.Create(logPath)
	if err != nil {
		return err
	}
	stderrPath := logPath + ".stderr"
	stderr, err := os.Create(stderrPath)
	if err != nil {
		_ = stdout.Close()
		return err
	}
	timeout := time.Duration(cfg.TimeoutMinutes) * time.Minute
	if timeout <= 0 {
		timeout = 30 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, cfg.Program, args...)
	configureCommandCancellation(cmd)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stderr = stderr
	var runErr error
	var stdoutErr error
	if cfg.Backend == "omp" {
		// OMP's stream is filtered through a line-buffered Stdout writer
		// rather than a StdoutPipe + goroutine: os/exec documents that
		// calling Wait before all pipe reads complete is incorrect (Wait
		// closes the pipe on process exit, which can truncate the final
		// message_end event and turn a successful build into a technical
		// failure). With cmd.Stdout set, exec's own copier drains the pipe
		// and Wait blocks on it, bounded by WaitDelay if an orphaned
		// descendant holds stdout open.
		compact := &ompCompactWriter{dst: stdout}
		cmd.Stdout = compact
		runErr = runOwnedCommand(cmd)
		stdoutErr = compact.Flush()
	} else {
		cmd.Stdout = stdout
		runErr = runOwnedCommand(cmd)
	}
	closeStdoutErr := stdout.Close()
	if stdoutErr == nil {
		stdoutErr = closeStdoutErr
	}
	stderrErr := stderr.Close()
	if runCtx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("%s timed out after %s (log: %s)", cfg.Backend, timeout, logPath)
	}
	if runErr != nil {
		return fmt.Errorf("%s failed: %w (stdout: %s; stderr: %s)", cfg.Backend, runErr, logPath, stderrPath)
	}
	if stdoutErr != nil {
		return stdoutErr
	}
	return stderrErr
}

// ompMaxBufferedLine bounds how much of one line the compact writer holds
// while looking for its newline. A line that outgrows it degrades to raw
// passthrough (bytes survive uncompacted) instead of failing the run like
// the old 16MiB-capped scanner, and instead of buffering without bound.
const ompMaxBufferedLine = 16 * 1024 * 1024

// ompCompactWriter drops OMP's message_update lines on the way to the log.
// The JSON stream repeats the entire accumulated assistant message in every
// message_update delta; persisting those redundant partial snapshots makes a
// normal build trace grow quadratically (hundreds of MB for one candidate).
// Turn/tool/final events pass through byte-for-byte; extractOMPFinal reads
// the preserved message_end event.
type ompCompactWriter struct {
	dst     io.Writer
	pending []byte
	// passthrough marks that the current line exceeded ompMaxBufferedLine
	// and is being streamed raw until its newline arrives.
	passthrough bool
	// err is sticky: after a destination write fails (possibly partially),
	// nothing may be re-emitted — a Flush retry would duplicate the line's
	// already-written prefix and corrupt the log evidence.
	err error
}

func (w *ompCompactWriter) Write(p []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	// io.Copy demands n == len(p) on success; p is re-sliced below, so the
	// consumed total is captured up front.
	total := len(p)
	if w.passthrough {
		idx := bytes.IndexByte(p, '\n')
		if idx < 0 {
			if _, err := w.dst.Write(p); err != nil {
				w.err = err
				return 0, err
			}
			return total, nil
		}
		if _, err := w.dst.Write(p[:idx+1]); err != nil {
			w.err = err
			return 0, err
		}
		w.passthrough = false
		p = p[idx+1:]
	}
	w.pending = append(w.pending, p...)
	for {
		idx := bytes.IndexByte(w.pending, '\n')
		if idx < 0 {
			if len(w.pending) > ompMaxBufferedLine {
				if _, err := w.dst.Write(w.pending); err != nil {
					w.err = err
					return 0, err
				}
				w.pending = nil
				w.passthrough = true
			}
			return total, nil
		}
		line := w.pending[:idx+1]
		if err := w.emit(line); err != nil {
			w.err = err
			return 0, err
		}
		w.pending = w.pending[idx+1:]
	}
}

// Flush forwards a final line that arrived without a trailing newline. Call
// after the producing process has exited.
func (w *ompCompactWriter) Flush() error {
	if w.err != nil {
		return w.err
	}
	if len(w.pending) == 0 {
		return nil
	}
	line := w.pending
	w.pending = nil
	if err := w.emit(line); err != nil {
		w.err = err
		return err
	}
	return nil
}

func (w *ompCompactWriter) emit(line []byte) error {
	var envelope struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(bytes.TrimRight(line, "\r\n"), &envelope) == nil && envelope.Type == "message_update" {
		return nil
	}
	_, err := w.dst.Write(line)
	return err
}

func extractOMPFinal(logPath, outputPath string) error {
	f, err := os.Open(logPath)
	if err != nil {
		return err
	}
	defer f.Close()
	var final string
	// ReadBytes instead of a capped Scanner: a large final assistant
	// message must be extracted, not fail the run with ErrTooLong.
	reader := bufio.NewReaderSize(f, 64*1024)
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			var event struct {
				Type    string `json:"type"`
				Message struct {
					Role    string `json:"role"`
					Content []struct {
						Type string `json:"type"`
						Text string `json:"text"`
					} `json:"content"`
				} `json:"message"`
			}
			if json.Unmarshal(line, &event) == nil && event.Type == "message_end" && event.Message.Role == "assistant" {
				var parts []string
				for _, content := range event.Message.Content {
					if content.Type == "text" {
						parts = append(parts, content.Text)
					}
				}
				if len(parts) > 0 {
					final = strings.Join(parts, "\n")
				}
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	if strings.TrimSpace(final) == "" {
		return fmt.Errorf("omp produced no final assistant message (log: %s)", logPath)
	}
	return os.WriteFile(outputPath, []byte(final), 0o644)
}

func extractClaudeFinal(logPath, outputPath string) error {
	b, err := os.ReadFile(logPath)
	if err != nil {
		return err
	}
	var result struct {
		Result           string          `json:"result"`
		StructuredOutput json.RawMessage `json:"structured_output"`
		IsError          bool            `json:"is_error"`
	}
	if err := json.Unmarshal(b, &result); err != nil {
		return fmt.Errorf("parse Claude JSON output: %w", err)
	}
	if result.IsError {
		return fmt.Errorf("claude returned an error result: %s", result.Result)
	}
	output := []byte(result.Result)
	if len(result.StructuredOutput) > 0 && string(result.StructuredOutput) != "null" {
		output = result.StructuredOutput
	}
	if strings.TrimSpace(string(output)) == "" {
		return fmt.Errorf("claude produced no final output (log: %s)", logPath)
	}
	return os.WriteFile(outputPath, output, 0o644)
}

func agentVersion(ctx context.Context, cfg AgentConfig) (string, error) {
	versionCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	args := append(append([]string(nil), cfg.PrefixArgs...), "--version")
	out, err := exec.CommandContext(versionCtx, cfg.Program, args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("resolve %s version: %w (%s)", cfg.Backend, err, strings.TrimSpace(string(out)))
	}
	version := strings.TrimSpace(string(out))
	if version == "" {
		return "", fmt.Errorf("resolve %s version: empty output", cfg.Backend)
	}
	return version, nil
}

func agentEnvironment(cfg AgentConfig, codexHome string) []string {
	if cfg.Backend == "codex" {
		return codexEnvironment(codexHome)
	}
	// Machine-specific TLS interception (corporate/AV root CAs) is the
	// host's concern: export NODE_EXTRA_CA_CERTS in the runner's
	// environment and it passes through untouched.
	env := filteredAgentEnvironment(cfg.Backend)
	sort.Strings(env)
	return env
}

func filteredAgentEnvironment(backend string) []string {
	env := make([]string, 0, len(os.Environ()))
	for _, entry := range os.Environ() {
		name, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		upper := strings.ToUpper(name)
		if strings.HasPrefix(upper, "CODEX_") || (looksCredentialBearing(upper) && !backendCredentialAllowed(backend, upper)) {
			continue
		}
		env = append(env, entry)
	}
	return env
}

func looksCredentialBearing(name string) bool {
	if name == "SSH_AUTH_SOCK" || name == "GPG_AGENT_INFO" {
		return true
	}
	for _, prefix := range []string{"AWS_", "AZURE_", "GCP_", "GOOGLE_", "GH_", "GITHUB_", "NPM_", "NUGET_", "DOCKER_", "SLACK_", "STRIPE_", "TWILIO_"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	for _, fragment := range []string{"API_KEY", "ACCESS_KEY", "AUTH_TOKEN", "CREDENTIAL", "PASSWORD", "PRIVATE_KEY", "SECRET", "SIGNING_KEY"} {
		if strings.Contains(name, fragment) {
			return true
		}
	}
	return name == "DATABASE_URL"
}

func backendCredentialAllowed(backend, name string) bool {
	switch backend {
	case "claude":
		return name == "ANTHROPIC_API_KEY"
	case "omp":
		return strings.HasPrefix(name, "OMP_") || strings.HasPrefix(name, "ZAI_") || strings.HasPrefix(name, "ZHIPU_") || strings.HasPrefix(name, "OPENAI_")
	default:
		return false
	}
}
