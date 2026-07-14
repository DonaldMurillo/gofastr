package evalrunner

import (
	"bufio"
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
		pipe, pipeErr := cmd.StdoutPipe()
		if pipeErr != nil {
			_ = stdout.Close()
			_ = stderr.Close()
			return pipeErr
		}
		cleanup, startErr := startOwnedCommand(cmd)
		if startErr != nil {
			_ = stdout.Close()
			_ = stderr.Close()
			return startErr
		}
		copyDone := make(chan error, 1)
		go func() { copyDone <- copyCompactOMPLog(stdout, pipe) }()
		runErr = cmd.Wait()
		cleanup()
		stdoutErr = <-copyDone
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

// OMP's JSON stream repeats the entire accumulated assistant message in every
// message_update delta. Persisting those redundant partial snapshots makes a
// normal build trace grow quadratically (hundreds of MB for one candidate).
// Keep turn/tool/final events byte-for-byte and drop only message_update lines;
// extractOMPFinal reads the preserved message_end event.
func copyCompactOMPLog(dst io.Writer, src io.Reader) error {
	scanner := bufio.NewScanner(src)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		var envelope struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(line, &envelope) == nil && envelope.Type == "message_update" {
			continue
		}
		if _, err := dst.Write(append(append([]byte(nil), line...), '\n')); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func extractOMPFinal(logPath, outputPath string) error {
	f, err := os.Open(logPath)
	if err != nil {
		return err
	}
	defer f.Close()
	var final string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for scanner.Scan() {
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
		if json.Unmarshal(scanner.Bytes(), &event) != nil || event.Type != "message_end" || event.Message.Role != "assistant" {
			continue
		}
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
	if err := scanner.Err(); err != nil {
		return err
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
	env := filteredAgentEnvironment(cfg.Backend)
	hasNodeCert := false
	for _, entry := range env {
		name, _, _ := strings.Cut(entry, "=")
		if strings.EqualFold(name, "NODE_EXTRA_CA_CERTS") {
			hasNodeCert = true
		}
	}
	if cfg.Backend == "omp" && !hasNodeCert {
		if home, err := os.UserHomeDir(); err == nil {
			cert := filepath.Join(home, ".omp", "norton-webshield-root.pem")
			if _, err := os.Stat(cert); err == nil {
				env = append(env, "NODE_EXTRA_CA_CERTS="+cert)
			}
		}
	}
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
