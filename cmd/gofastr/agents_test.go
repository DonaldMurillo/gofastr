package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// AGENTS.md is now a thin TOC — full per-section content lives under
// agents/. The TOC must reference every inventory entry by section
// title and link to its detail file, but the FULL body must NOT inline.
func TestBuildAgentsMDIsThinTOC(t *testing.T) {
	got := buildAgentsMD()
	body := string(got)

	mustContain(t, body, "# AGENTS.md")
	mustContain(t, body, agentsAutoStart)
	mustContain(t, body, agentsAutoEnd)
	mustContain(t, body, "## Project conventions") // tail preserved

	// TOC header + at least one row.
	mustContain(t, body, "| Section | Use this when | Details |")
	mustContain(t, body, "**framework**")         // row exists
	mustContain(t, body, "**ui**")                // component-catalog row exists
	mustContain(t, body, "**battery/admin**")     // row exists
	mustContain(t, body, "(agents/framework.md)") // link to detail
	mustContain(t, body, "(agents/ui.md)")
	mustContain(t, body, "(agents/battery-admin.md)")

	// AGENTS.md must NOT inline the full per-section bodies anymore.
	// `**Shape:**` only appears in battery agents.md bodies (never in
	// the preamble) — its presence here would mean we regressed to
	// inlining. `**Import:**` is the same.
	if strings.Contains(body, "**Shape:**") {
		t.Fatalf("AGENTS.md inlines '**Shape:**' code blocks — those belong in agents/<name>.md detail files")
	}
	if strings.Contains(body, "**Import:**") {
		t.Fatalf("AGENTS.md inlines '**Import:**' lines — those belong in agents/<name>.md detail files")
	}

	// Size sanity: TOC should be small. Full inlined version was ~500
	// lines; the TOC should be well under 100.
	lines := strings.Count(body, "\n")
	if lines > 100 {
		t.Fatalf("AGENTS.md = %d lines — TOC bloat regressed past 100", lines)
	}
}

// writeAgentDetailFiles must drop one file per inventory entry under
// <dir>/agents/, each containing the full body of that entry's
// agents.md plus a back-link.
func TestWriteAgentDetailFilesPopulatesDir(t *testing.T) {
	dir := t.TempDir()
	if err := writeAgentDetailFiles(dir); err != nil {
		t.Fatalf("writeAgentDetailFiles: %v", err)
	}
	for _, name := range []string{
		"agents/framework.md",
		"agents/ui.md",
		"agents/battery-admin.md",
		"agents/battery-auth.md",
		"agents/battery-log.md", // the one we added
	} {
		path := filepath.Join(dir, name)
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		// Detail files must contain the full body (proxied by the
		// presence of the "Use this when" heading the TOC strips).
		if !strings.Contains(string(body), "**Use this when**") {
			t.Errorf("%s missing '**Use this when**' heading — detail file is empty or truncated", name)
		}
		// And the back-link to AGENTS.md so an agent landing here can
		// pivot back to the index.
		if !strings.Contains(string(body), "[AGENTS.md](../AGENTS.md)") {
			t.Errorf("%s missing back-link to ../AGENTS.md", name)
		}
	}
}

// User has their own agents/some-doc.md sitting alongside ours;
// writeAgentDetailFiles must NOT touch it. Coexistence — we own
// our filenames, they own theirs.
func TestWriteAgentDetailFilesLeavesUnrelatedUserFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	userPath := filepath.Join(dir, "agents", "my-onboarding.md")
	userContent := []byte("# My team's onboarding\n\nhand-written\n")
	if err := os.WriteFile(userPath, userContent, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := writeAgentDetailFiles(dir); err != nil {
		t.Fatalf("writeAgentDetailFiles: %v", err)
	}

	got, err := os.ReadFile(userPath)
	if err != nil {
		t.Fatalf("user file vanished: %v", err)
	}
	if string(got) != string(userContent) {
		t.Fatalf("user file mutated:\n%s", got)
	}
}

// User has agents/framework.md as their own hand-written doc (name
// collision with our generated file). writeAgentDetailFiles must
// REFUSE to clobber — return an error naming the conflicting path
// and suggesting a fix. Better a loud failure than silent overwrite.
func TestWriteAgentDetailFilesRefusesToClobberUserFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	collisionPath := filepath.Join(dir, "agents", "framework.md")
	userBody := []byte("# my hand-written framework notes\n\nDO NOT CLOBBER\n")
	if err := os.WriteFile(collisionPath, userBody, 0o644); err != nil {
		t.Fatal(err)
	}

	err := writeAgentDetailFiles(dir)
	if err == nil {
		t.Fatal("expected error when generated path would clobber a user file")
	}
	if !strings.Contains(err.Error(), "framework.md") {
		t.Fatalf("error doesn't name the conflicting file: %v", err)
	}

	got, _ := os.ReadFile(collisionPath)
	if string(got) != string(userBody) {
		t.Fatalf("user file was clobbered despite the error:\n%s", got)
	}
}

// A previous-sync output is detected by the AUTO-GENERATED header and
// gets overwritten freely. That's how `gofastr agents sync` works
// every cycle.
func TestWriteAgentDetailFilesOverwritesPreviousAutoGen(t *testing.T) {
	dir := t.TempDir()
	if err := writeAgentDetailFiles(dir); err != nil {
		t.Fatalf("first write: %v", err)
	}
	// Second call must succeed (overwrites all of our own files).
	if err := writeAgentDetailFiles(dir); err != nil {
		t.Fatalf("second write: %v", err)
	}
}

// Detail files must announce they're auto-generated AND name their
// canonical source so a user finding them knows where to edit.
func TestWriteAgentDetailFilesIncludeAutoGenHeader(t *testing.T) {
	dir := t.TempDir()
	if err := writeAgentDetailFiles(dir); err != nil {
		t.Fatalf("writeAgentDetailFiles: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "agents/battery-admin.md"))
	if err != nil {
		t.Fatalf("read battery-admin.md: %v", err)
	}
	s := string(body)
	if !strings.HasPrefix(s, "<!--") {
		t.Fatalf("detail file must start with an HTML comment header so the warning is the first thing a reader sees; got:\n%s", s[:100])
	}
	for _, want := range []string{
		"AUTO-GENERATED",
		"gofastr agents sync",
		"github.com/DonaldMurillo/gofastr/battery/admin", // names the source
	} {
		if !strings.Contains(s, want) {
			t.Errorf("detail header missing %q", want)
		}
	}
}

// extractUseWhen pulls the trigger phrases out of a battery's
// agents.md so the TOC row stays short.
func TestExtractUseWhenStripsBoilerplate(t *testing.T) {
	cases := map[string]string{
		"**Use this when** the prompt mentions: foo, bar, baz.\n\nnext para": "foo, bar, baz",
		"**Use this when** the prompt mentions: just one\n\n":                "just one",
		"**Use this when** anything goes here\n\n":                           "anything goes here",
		"no use-when line": "—",
		// Multi-line paragraph — markdown wraps the trigger list across
		// lines; the TOC must show all of them, not just the first wrap.
		"**Use this when** the prompt mentions: a, b,\nc, d, e\n\nrest": "a, b, c, d, e",
		// Author wrote the next **Heading:** immediately without a
		// blank line — the use-when paragraph still ends at the new
		// heading, otherwise the TOC cell would slurp the whole
		// **Import:** block too.
		"**Use this when** trigger phrases here\n**Import:** `pkg`\n": "trigger phrases here",
		// And the abbreviated case where the next bolded heading
		// shows up on the line right after the use-when phrase.
		"**Use this when** foo, bar\n**Shape:**\n": "foo, bar",
	}
	for in, want := range cases {
		got := extractUseWhen(in)
		if got != want {
			t.Errorf("extractUseWhen(%q) = %q, want %q", in, got, want)
		}
	}
}

// Walks battery/ on disk and asserts every battery dir is represented
// in the generated AGENTS.md TOC + has a detail file under agents/.
// Catches the "new battery shipped without agents.md / blank import"
// failure mode that the old hard-coded allow-list ratified instead of
// detecting.
func TestEveryBatteryDirHasAgentsSection(t *testing.T) {
	tocBody := string(buildAgentsMD())
	detailDir := t.TempDir()
	if err := writeAgentDetailFiles(detailDir); err != nil {
		t.Fatalf("writeAgentDetailFiles: %v", err)
	}

	entries, err := os.ReadDir("../../battery")
	if err != nil {
		t.Fatalf("read battery/: %v", err)
	}
	allowed := map[string]bool{
		// Pre-1.0 surfaces. If you intend an exclusion, justify it here.
		"experimental": true,
		"embed":        true,
	}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if allowed[e.Name()] {
			continue
		}
		// TOC row + detail-link.
		tocLink := "agents/battery-" + e.Name() + ".md"
		if !strings.Contains(tocBody, tocLink) {
			t.Errorf("battery/%s/ exists but TOC entry linking to %s is missing from AGENTS.md — add agents.{go,md} and blank-import the package in cmd/gofastr/agents.go (or add it to the allow-list with a reason)", e.Name(), tocLink)
		}
		// Detail file present + non-empty.
		detailPath := filepath.Join(detailDir, "agents", "battery-"+e.Name()+".md")
		if info, err := os.Stat(detailPath); err != nil || info.Size() == 0 {
			t.Errorf("battery/%s/ detail file %s missing or empty", e.Name(), detailPath)
		}
	}
}

// The on-disk agents.md content must actually reach the generated
// detail file under agents/ (catches a blank-import that fires init()
// but ends up with empty Markdown because the embed path is stale).
func TestBatteryAgentsMDContentReachesDetailFile(t *testing.T) {
	detailDir := t.TempDir()
	if err := writeAgentDetailFiles(detailDir); err != nil {
		t.Fatalf("writeAgentDetailFiles: %v", err)
	}

	entries, err := os.ReadDir("../../battery")
	if err != nil {
		t.Fatalf("read battery/: %v", err)
	}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		mdPath := filepath.Join("../../battery", e.Name(), "agents.md")
		raw, err := os.ReadFile(mdPath)
		if err != nil {
			continue // no agents.md ⇒ caught by completeness test above
		}
		head := strings.SplitN(string(raw), "\n", 2)[0]
		if head == "" {
			continue
		}
		detailPath := filepath.Join(detailDir, "agents", "battery-"+e.Name()+".md")
		body, err := os.ReadFile(detailPath)
		if err != nil {
			continue
		}
		if !strings.Contains(string(body), head) {
			t.Errorf("battery/%s/agents.md heading %q not found in detail file %s — is agents.go blank-imported in cmd/gofastr/agents.go?", e.Name(), head, detailPath)
		}
	}
}

func TestRefreshAgentsMDPreservesTailAndPreamble(t *testing.T) {
	original := buildAgentsMD()
	// Pretend the user customised the preamble + tail with their own content.
	customised := bytes.Replace(original, []byte("# AGENTS.md"), []byte("# AGENTS.md (Customised header)"), 1)
	customised = bytes.Replace(customised, []byte("## Project conventions"), []byte("## Project conventions\n\nHand-written rule: be careful.\n\n## Project conventions"), 1)

	out, changed, err := refreshAgentsMD(customised)
	if err != nil {
		t.Fatalf("refreshAgentsMD: %v", err)
	}
	_ = changed
	if !bytes.Contains(out, []byte("Customised header")) {
		t.Fatal("custom preamble lost during sync")
	}
	if !bytes.Contains(out, []byte("Hand-written rule: be careful.")) {
		t.Fatal("custom tail content lost during sync")
	}
}

func TestRefreshAgentsMDFailsWithoutMarkers(t *testing.T) {
	_, _, err := refreshAgentsMD([]byte("# manually-written AGENTS.md\n\nno markers here"))
	if err == nil {
		t.Fatal("expected error when markers are missing")
	}
	if !strings.Contains(err.Error(), "marker") {
		t.Fatalf("error doesn't mention marker: %v", err)
	}
}

// User-quoted markers in the preamble (e.g. an explanatory paragraph)
// must not corrupt the file. The refresh must refuse to operate when
// there's more than one occurrence of either marker.
func TestRefreshAgentsMDRefusesDuplicateMarkers(t *testing.T) {
	custom := []byte(`# AGENTS.md
## How this file works
The markers ` + agentsAutoStart + ` and ` + agentsAutoEnd + ` bracket auto content.

` + agentsAutoStart + `
auto content here
` + agentsAutoEnd + `

tail
`)
	_, _, err := refreshAgentsMD(custom)
	if err == nil {
		t.Fatal("expected refresh to refuse when markers occur more than once")
	}
	if !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("error doesn't mention 'exactly one' (saw: %v)", err)
	}
}

// CRLF-encoded files (Windows clones with autocrlf=true) must not
// register as "changed" after a refresh that produces the same logical
// content with LF line endings.
func TestRefreshAgentsMDHandlesCRLFWithoutFlapping(t *testing.T) {
	original := buildAgentsMD()
	crlf := bytes.ReplaceAll(original, []byte("\n"), []byte("\r\n"))

	_, changed, err := refreshAgentsMD(crlf)
	if err != nil {
		t.Fatalf("refresh on CRLF input: %v", err)
	}
	if changed {
		t.Fatal("CRLF input flagged as changed against LF-emitted regeneration — normalize line endings before comparing")
	}
}

func TestRefreshAgentsMDIsIdempotent(t *testing.T) {
	original := buildAgentsMD()
	out, changed, err := refreshAgentsMD(original)
	if err != nil {
		t.Fatalf("refreshAgentsMD: %v", err)
	}
	if changed {
		t.Fatal("refreshing a freshly-generated doc reported a change")
	}
	if !bytes.Equal(normalizeMD(out), normalizeMD(original)) {
		t.Fatal("refreshed bytes differ from original")
	}
}

func mustContain(t *testing.T, body, needle string) {
	t.Helper()
	if !strings.Contains(body, needle) {
		t.Fatalf("AGENTS.md missing %q:\n%s", needle, body)
	}
}
