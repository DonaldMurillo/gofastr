package session

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/internal/jsonredact"
	"github.com/DonaldMurillo/gofastr/internal/fileperm"
)

// RedactLevel controls how aggressively the export bundle redacts.
type RedactLevel int

const (
	// RedactStrict replaces every payload with {"redacted":"strict"}.
	RedactStrict RedactLevel = iota
	// RedactStandard exports payloads as stored, after the store's on-write redaction.
	RedactStandard
	// RedactMaintainer runs the deep secret patterns over stored payloads and adds redactions.json.
	RedactMaintainer
)

// ExportBundle writes one session as a zip of events.jsonl, a bundle.json manifest and, at RedactMaintainer, redactions.json.
type ExportBundle struct {
	Store   Store
	Session ids.SessionID
	Profile string
	Model   string
	Level   RedactLevel
	OutPath string
}

const exportPage = 1000

// Write writes the bundle to OutPath, owner-only, and returns that path.
func (e *ExportBundle) Write(ctx context.Context) (string, error) {
	if e.OutPath == "" {
		return "", fmt.Errorf("export: OutPath required")
	}
	tmp := e.OutPath + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return "", err
	}
	if err := fileperm.RestrictOpenFile(f); err != nil {
		_ = f.Close()
		return "", err
	}
	zw := zip.NewWriter(f)
	defer func() {
		_ = zw.Close()
		_ = f.Close()
	}()

	report := newRedactionReport()
	jsonl, err := zw.Create("events.jsonl")
	if err != nil {
		return "", err
	}
	var since uint64
	skipped := 0
	for {
		events, err := e.Store.EventsSince(ctx, e.Session, since, exportPage)
		if err != nil {
			return "", err
		}
		if len(events) == 0 {
			break
		}
		for _, env := range events {
			out := applyExportRedaction(env, e.Level, report)
			body, err := json.Marshal(out)
			if err != nil {
				skipped++
				continue
			}
			if _, err := jsonl.Write(append(body, '\n')); err != nil {
				return "", err
			}
		}
		last := events[len(events)-1].ID
		if last <= since {
			return "", fmt.Errorf("session export: EventsSince returned id %d after %d", last, since)
		}
		since = last
	}

	manifest := map[string]any{
		"schema_version": 1,
		"session_id":     string(e.Session),
		"profile":        e.Profile,
		"model":          e.Model,
		"redact_level":   levelString(e.Level),
		"exported_at":    time.Now().UTC().Format(time.RFC3339),
		"skipped_events": skipped,
	}
	if err := writeJSON(zw, "bundle.json", manifest); err != nil {
		return "", err
	}

	if e.Level == RedactMaintainer {
		if err := writeJSON(zw, "redactions.json", report); err != nil {
			return "", err
		}
	}

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

func applyExportRedaction(env control.EventEnvelope, level RedactLevel, report *redactionReport) control.EventEnvelope {
	if level == RedactStrict {
		env.Payload = []byte(`{"redacted":"strict"}`)
		return env
	}
	if level == RedactMaintainer {
		redact := func(text string) string {
			out, hits := deepRedact(text)
			for k, v := range hits {
				report.Counts[k] += v
			}
			return out
		}
		if out, ok := jsonredact.Strings(env.Payload, redact); ok {
			env.Payload = out
		} else {
			env.Payload = []byte(redact(string(env.Payload)))
		}
	}
	return env
}

type redactionReport struct {
	Counts map[string]int `json:"counts"`
}

func newRedactionReport() *redactionReport {
	return &redactionReport{Counts: make(map[string]int)}
}

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
