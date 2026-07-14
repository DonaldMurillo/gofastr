package evalrunner

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCopyCompactOMPLogDropsOnlyRedundantMessageUpdates(t *testing.T) {
	input := strings.Join([]string{
		`{"type":"turn_start"}`,
		`{"type":"message_update","assistantMessageEvent":{"partial":"growing"}}`,
		`{"type":"tool_execution_end","toolName":"bash"}`,
		`{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"done"}]}}`,
	}, "\n") + "\n"
	var output bytes.Buffer
	if err := copyCompactOMPLog(&output, strings.NewReader(input)); err != nil {
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
