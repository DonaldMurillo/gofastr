package evalrunner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOMPCompactWriterDropsOnlyMessageUpdates(t *testing.T) {
	input := strings.Join([]string{
		`{"type":"turn_start"}`,
		`{"type":"message_update","assistantMessageEvent":{"partial":"growing"}}`,
		`{"type":"tool_execution_end","toolName":"bash"}`,
		`{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"done"}]}}`,
	}, "\n") + "\n"
	var output bytes.Buffer
	w := &ompCompactWriter{dst: &output}
	// Feed in awkward chunks so lines straddle Write calls, the way a pipe
	// delivers them.
	for _, chunk := range []string{input[:7], input[7:40], input[40:]} {
		if _, err := w.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "message_update") {
		t.Fatalf("redundant OMP partial survived:\n%s", output.String())
	}
	for _, event := range []string{"turn_start", "tool_execution_end", "message_end"} {
		if !strings.Contains(output.String(), event) {
			t.Fatalf("OMP compact log lost %s:\n%s", event, output.String())
		}
	}
}

func TestOMPCompactWriterFlushKeepsUnterminatedFinalLine(t *testing.T) {
	var output bytes.Buffer
	w := &ompCompactWriter{dst: &output}
	if _, err := w.Write([]byte(`{"type":"message_end","message":{"role":"assistant"}}`)); err != nil {
		t.Fatal(err)
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "message_end") {
		t.Fatalf("final unterminated line lost on flush:\n%s", output.String())
	}
}

// A destination write error must be sticky: Flush after a failed (possibly
// partial) line write must NOT re-emit the line — duplicated partial bytes
// would corrupt the log evidence someone inspects to debug the failed run.
func TestOMPCompactWriterErrorIsSticky(t *testing.T) {
	failing := &failAfterWriter{limit: 4}
	w := &ompCompactWriter{dst: failing}
	if _, err := w.Write([]byte("{\"type\":\"turn_start\"}\n")); err == nil {
		t.Fatal("expected destination write error")
	}
	if err := w.Flush(); err == nil {
		t.Fatal("Flush after a write error must return the sticky error, not retry")
	}
	if got := failing.buf.String(); strings.Count(got, "{") > 1 {
		t.Fatalf("failed line was re-emitted after its partial write:\n%q", got)
	}
}

type failAfterWriter struct {
	buf   bytes.Buffer
	limit int
}

func (f *failAfterWriter) Write(p []byte) (int, error) {
	if f.buf.Len()+len(p) > f.limit {
		n := f.limit - f.buf.Len()
		if n > 0 {
			f.buf.Write(p[:n])
		}
		return n, fmt.Errorf("disk full")
	}
	f.buf.Write(p)
	return len(p), nil
}

// A pathological newline-free blob must not grow the line buffer without
// bound (the runner would be OOM-killed mid-suite, losing every candidate).
// Past the cap the writer degrades to raw passthrough for that line: the
// bytes survive uncompacted instead of failing the run like the old
// 16MiB-capped scanner did.
func TestOMPCompactWriterBoundsBufferedLine(t *testing.T) {
	var output bytes.Buffer
	w := &ompCompactWriter{dst: &output}
	blob := bytes.Repeat([]byte("x"), ompMaxBufferedLine+4096)
	if _, err := w.Write(blob); err != nil {
		t.Fatal(err)
	}
	if len(w.pending) > ompMaxBufferedLine {
		t.Fatalf("pending grew past the cap: %d", len(w.pending))
	}
	if _, err := w.Write([]byte("tail\n{\"type\":\"turn_start\"}\n")); err != nil {
		t.Fatal(err)
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	if !strings.HasPrefix(got, "xxxx") || !strings.Contains(got, "tail\n") || !strings.Contains(got, "turn_start") {
		t.Fatalf("oversized line must pass through raw and filtering must resume on the next line; got %d bytes, tail=%q", len(got), got[len(got)-64:])
	}
}

func TestClaudeCompatibleSchemaRemovesUnsupportedDialectOnly(t *testing.T) {
	input := []byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"score":{"type":"number","minimum":0}}}`)
	got, err := claudeCompatibleSchema(input)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(got, &document); err != nil {
		t.Fatal(err)
	}
	if _, exists := document["$schema"]; exists {
		t.Fatalf("Claude schema still contains unsupported $schema: %s", got)
	}
	for _, key := range []string{"type", "properties"} {
		if _, exists := document[key]; !exists {
			t.Fatalf("Claude schema lost %q: %s", key, got)
		}
	}
}

func TestNormalizeAgentConfigOMPIsBuilderOnly(t *testing.T) {
	builder := AgentConfig{Backend: "OMP", Model: "glm-5.2"}
	if err := normalizeAgentConfig("builder", &builder, 45); err != nil {
		t.Fatalf("normalize OMP builder: %v", err)
	}
	if builder.Backend != "omp" || builder.Program != "omp" || builder.Effort != "xhigh" || builder.TimeoutMinutes != 45 {
		t.Fatalf("unexpected OMP defaults: %+v", builder)
	}
	judge := AgentConfig{Backend: "omp", Model: "glm-5.2"}
	if err := normalizeAgentConfig("judge", &judge, 12); err == nil || !strings.Contains(err.Error(), "images=no") {
		t.Fatalf("OMP judge must be rejected with the image limitation, got %v", err)
	}
}

func TestApplyAgentOptionsUsesBackendModelAliases(t *testing.T) {
	cfg := AgentConfig{Backend: "codex", Program: "codex", Model: "codex-model"}
	applyAgentOptions(&cfg, "omp", "", nil, "")
	if cfg.Program != "omp" || cfg.Model != "glm-5.2" {
		t.Fatalf("OMP shorthand not applied: %+v", cfg)
	}
	applyAgentOptions(&cfg, "claude", "", nil, "")
	if cfg.Program != "claude" || cfg.Model != "opus" {
		t.Fatalf("Claude shorthand not applied: %+v", cfg)
	}
}

func TestCodexSwitchNeedsExplicitModel(t *testing.T) {
	// There is no baked-in codex model alias: codex catalogs vary per
	// install and model IDs are provenance. Switching a claude/opus suite
	// agent to codex must not carry "opus" into a codex invocation — it
	// must clear the model and fail validation until a --*-model is given.
	cfg := AgentConfig{Backend: "claude", Program: "claude", Model: "opus", Effort: "high"}
	applyAgentOptions(&cfg, "codex", "", nil, "")
	if cfg.Backend != "codex" || cfg.Program != "codex" {
		t.Fatalf("codex switch incomplete: %+v", cfg)
	}
	if cfg.Model != "" {
		t.Fatalf("codex switch must not inherit the previous backend's model, got %q", cfg.Model)
	}
	if err := normalizeAgentConfig("judge", &cfg, 12); err == nil || !strings.Contains(err.Error(), "model") {
		t.Fatalf("codex switch without an explicit model must fail validation, got %v", err)
	}
	cfg = AgentConfig{Backend: "claude", Program: "claude", Model: "opus"}
	applyAgentOptions(&cfg, "codex", "", nil, "gpt-5-codex")
	if cfg.Model != "gpt-5-codex" {
		t.Fatalf("explicit model must pin the codex agent: %+v", cfg)
	}
	if err := normalizeAgentConfig("judge", &cfg, 12); err != nil {
		t.Fatalf("explicit codex model must validate: %v", err)
	}
}

func TestExtractOMPFinalMessage(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "omp.jsonl")
	outPath := filepath.Join(dir, "final.txt")
	log := "{\"type\":\"message_end\",\"message\":{\"role\":\"assistant\",\"content\":[{\"type\":\"text\",\"text\":\"finished naturally\"}]}}\n"
	if err := os.WriteFile(logPath, []byte(log), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := extractOMPFinal(logPath, outPath); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "finished naturally" {
		t.Fatalf("unexpected OMP final output %q", got)
	}
}

func TestExtractClaudeStructuredOutput(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "claude.json")
	outPath := filepath.Join(dir, "final.json")
	log := `{"result":"fallback","structured_output":{"candidate_id":"opaque","score":9},"is_error":false}`
	if err := os.WriteFile(logPath, []byte(log), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := extractClaudeFinal(logPath, outPath); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"candidate_id":"opaque","score":9}` {
		t.Fatalf("unexpected Claude structured output %s", got)
	}
}

func TestBuilderPromptTimeboxesVerificationWithoutPrescribingDesign(t *testing.T) {
	prompt := builderPrompt()
	for _, want := range []string{"25-minute implementation budget", "at most two bounded visual-review passes", "Stop every local server"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("builder prompt missing operational bound %q", want)
		}
	}
	for _, forbidden := range []string{"RecordSummary", "MetricBand", "support rail", "avatar"} {
		if strings.Contains(prompt, forbidden) {
			t.Errorf("neutral builder prompt leaked design direction %q", forbidden)
		}
	}
}
