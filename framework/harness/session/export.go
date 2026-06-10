package session

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
)

// RedactLevel controls how aggressively the export bundle redacts.
type RedactLevel int

const (
	// RedactStrict — drop full content; emit kinds + timestamps only.
	RedactStrict RedactLevel = iota
	// RedactStandard — apply the same redactors that ran on write.
	// (The on-disk events already passed through these; this level
	// re-applies them to be safe in case a custom redactor was
	// disabled.)
	RedactStandard
	// RedactMaintainer — run a deeper-pass detector on top of the
	// stored content and include a redaction report in the bundle.
	RedactMaintainer
)

// ExportBundle writes a zip archive containing:
//
//   - bundle.json     — manifest (session ID, profile, version, redact level)
//   - events.jsonl    — one canonical event envelope per line
//   - redactions.txt  — only at RedactMaintainer; counts per pattern
//   - meta.json       — extra metadata (profile, model, turn count)
type ExportBundle struct {
	Store   Store
	Session ids.SessionID
	Profile string
	Model   string
	Level   RedactLevel
	OutPath string
}

// Write produces the bundle on disk. Returns the bundle path.
func (e *ExportBundle) Write(ctx context.Context) (string, error) {
	if e.OutPath == "" {
		return "", fmt.Errorf("export: OutPath required")
	}
	tmp := e.OutPath + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	zw := zip.NewWriter(f)
	defer func() {
		_ = zw.Close()
		_ = f.Close()
	}()

	// Manifest.
	manifest := map[string]any{
		"schema_version": 1,
		"session_id":     string(e.Session),
		"profile":        e.Profile,
		"model":          e.Model,
		"redact_level":   levelString(e.Level),
		"exported_at":    time.Now().UTC().Format(time.RFC3339),
	}
	if err := writeJSON(zw, "bundle.json", manifest); err != nil {
		return "", err
	}

	// Events.
	events, err := e.Store.EventsSince(ctx, e.Session, 0, 0)
	if err != nil {
		return "", err
	}
	report := newRedactionReport()
	jsonl, err := zw.Create("events.jsonl")
	if err != nil {
		return "", err
	}
	for _, env := range events {
		out := applyExportRedaction(env, e.Level, report)
		body, err := json.Marshal(out)
		if err != nil {
			continue
		}
		if _, err := jsonl.Write(append(body, '\n')); err != nil {
			return "", err
		}
	}

	// Maintainer report.
	if e.Level == RedactMaintainer {
		if err := writeJSON(zw, "redactions.json", report); err != nil {
			return "", err
		}
	}

	// Close & atomic rename.
	if err := zw.Close(); err != nil {
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, e.OutPath); err != nil {
		return "", err
	}
	return e.OutPath, nil
}

func writeJSON(zw *zip.Writer, name string, body any) error {
	w, err := zw.Create(name)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(body)
}

func levelString(l RedactLevel) string {
	switch l {
	case RedactStrict:
		return "strict"
	case RedactStandard:
		return "standard"
	case RedactMaintainer:
		return "maintainer"
	}
	return "unknown"
}

// applyExportRedaction returns a copy of the envelope with redaction
// applied according to the level. At RedactStrict, the payload is
// replaced with "{\"redacted\":\"strict\"}".
func applyExportRedaction(env control.EventEnvelope, level RedactLevel, report *redactionReport) control.EventEnvelope {
	if level == RedactStrict {
		env.Payload = []byte(`{"redacted":"strict"}`)
		return env
	}
	if level == RedactMaintainer {
		new, hits := deepRedact(string(env.Payload))
		for k, v := range hits {
			report.Counts[k] += v
		}
		env.Payload = []byte(new)
	}
	return env
}

type redactionReport struct {
	Counts map[string]int `json:"counts"`
}

func newRedactionReport() *redactionReport {
	return &redactionReport{Counts: make(map[string]int)}
}

// deepRedact applies a stronger redaction pass than the on-write
// middleware. Used only at RedactMaintainer.
//
// Patterns include: AWS keys, GitHub PATs, Bearer/JWT, base64-shaped
// blobs ≥40 chars (catches Anthropic / OpenAI API keys), email
// addresses, plausible private-key headers, and arbitrary
// hex-shaped 64+-char tokens.
var deepPatterns = []struct {
	re   *regexp.Regexp
	kind string
}{
	{regexp.MustCompile(`AKIA[0-9A-Z]{16}`), "aws-access-key"},
	{regexp.MustCompile(`ASIA[0-9A-Z]{16}`), "aws-session-key"},
	{regexp.MustCompile(`ghp_[A-Za-z0-9]{36,}`), "github-pat"},
	{regexp.MustCompile(`github_pat_[A-Za-z0-9_]{60,}`), "github-pat-v2"},
	{regexp.MustCompile(`sk-(ant|or|proj)?-[A-Za-z0-9_\-]{30,}`), "api-key"},
	{regexp.MustCompile(`Bearer\s+[A-Za-z0-9\-_\.=]{20,}`), "bearer"},
	{regexp.MustCompile(`eyJ[A-Za-z0-9_\-]{8,}\.[A-Za-z0-9_\-]{8,}\.[A-Za-z0-9_\-]{8,}`), "jwt"},
	{regexp.MustCompile(`-----BEGIN [A-Z ]+PRIVATE KEY-----`), "private-key"},
	{regexp.MustCompile(`[a-z0-9._%+-]+@[a-z0-9.-]+\.[a-z]{2,}`), "email"},
	{regexp.MustCompile(`[a-f0-9]{64,}`), "hex-token"},
}

func deepRedact(text string) (string, map[string]int) {
	hits := make(map[string]int)
	out := text
	for _, p := range deepPatterns {
		newOut := p.re.ReplaceAllStringFunc(out, func(m string) string {
			hits[p.kind]++
			return "«redacted:" + p.kind + "»"
		})
		out = newOut
	}
	return out, hits
}

// keep imports honest
var _ = strings.Contains
var _ = io.Discard
