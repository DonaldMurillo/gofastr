package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/agentsinv"

	// Blank imports — each battery's init() registers its agents.md
	// snippet into agentsinv. Importing here is how the generator
	// "inventories" the framework. Adding a battery only requires
	// adding one line below + the agents.go/agents.md pair in the
	// battery itself.
	_ "github.com/DonaldMurillo/gofastr/battery/admin"
	_ "github.com/DonaldMurillo/gofastr/battery/auth"
	_ "github.com/DonaldMurillo/gofastr/battery/cache"
	_ "github.com/DonaldMurillo/gofastr/battery/email"
	_ "github.com/DonaldMurillo/gofastr/battery/log"
	_ "github.com/DonaldMurillo/gofastr/battery/notify"
	_ "github.com/DonaldMurillo/gofastr/battery/print"
	_ "github.com/DonaldMurillo/gofastr/battery/queue"
	_ "github.com/DonaldMurillo/gofastr/battery/search"
	_ "github.com/DonaldMurillo/gofastr/battery/storage"
	_ "github.com/DonaldMurillo/gofastr/battery/webhook"
	_ "github.com/DonaldMurillo/gofastr/framework"
)

const (
	agentsAutoStart = "<!-- gofastr-batteries:start -->"
	agentsAutoEnd   = "<!-- gofastr-batteries:end -->"
)

// runAgents dispatches the `gofastr agents <subcommand>` family.
func runAgents(args []string) {
	if len(args) == 0 {
		fail("Subcommand required.")
		info("Usage: gofastr agents [init|sync]")
		osExit(1)
	}
	switch args[0] {
	case "init":
		runAgentsInit(args[1:])
	case "sync":
		runAgentsSync(args[1:])
	case "skill":
		runAgentsSkill(args[1:])
	default:
		fail("Unknown subcommand %q.", args[0])
		info("Try `gofastr agents init`, `gofastr agents sync`, or `gofastr agents skill`.")
		osExit(1)
	}
}

// runAgentsInit writes a fresh AGENTS.md in the current directory (or
// the path given as the first argument). Errors if the file already
// exists — use `agents sync` to refresh.
func runAgentsInit(args []string) {
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}
	target := filepath.Join(dir, "AGENTS.md")
	if _, err := os.Stat(target); err == nil {
		fail("%s already exists. Use `gofastr agents sync` to refresh.", target)
		osExit(1)
	}
	if err := os.WriteFile(target, buildAgentsMD(), 0o644); err != nil {
		fail("Failed to write %s: %v", target, err)
		osExit(1)
	}
	if err := writeAgentDetailFiles(dir); err != nil {
		fail("Failed to write agents/ details: %v", err)
		osExit(1)
	}
	success("Wrote %s and %d detail file(s) under agents/", target, len(agentsinv.All()))
	warnMissingMarkdown()
}

// runAgentsSkill drops the gofastr-host Claude Code skill into the
// current project's .claude/skills/ tree. Useful when a host that
// pre-dates `gofastr init --skill` wants the skill retroactively.
func runAgentsSkill(args []string) {
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}
	if err := writeHostSkill(dir); err != nil {
		fail("Failed to write skill: %v", err)
		osExit(1)
	}
	success("Wrote %s/.claude/skills/gofastr-host/SKILL.md", dir)
}

// runAgentsSync refreshes the auto section between the markers without
// touching anything outside them. Lets host apps refresh after a
// framework upgrade.
func runAgentsSync(args []string) {
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}
	target := filepath.Join(dir, "AGENTS.md")
	existing, err := os.ReadFile(target)
	if err != nil {
		fail("Cannot read %s: %v", target, err)
		info("Run `gofastr agents init` first.")
		osExit(1)
	}
	refreshed, changed, err := refreshAgentsMD(existing)
	if err != nil {
		fail("Refresh failed: %v", err)
		osExit(1)
	}
	// Always refresh the detail files — they're framework-owned and
	// cheap to rewrite, and treating them as "changed" tracking would
	// duplicate the marker-protection logic for ten more files.
	if err := writeAgentDetailFiles(dir); err != nil {
		fail("Failed to write agents/ details: %v", err)
		osExit(1)
	}
	if !changed {
		info("%s TOC already up to date (agents/ details refreshed).", target)
		warnMissingMarkdown()
		return
	}
	if err := os.WriteFile(target, refreshed, 0o644); err != nil {
		fail("Failed to write %s: %v", target, err)
		osExit(1)
	}
	success("Refreshed %s and %d detail file(s) under agents/", target, len(agentsinv.All()))
	warnMissingMarkdown()
}

// warnMissingMarkdown prints a non-fatal advisory for every battery
// that registered itself with an empty agents.md. Surfaces here because
// `agents` is the subcommand a developer hits when they ship a new
// battery; the framework no longer panics at startup if Markdown is
// blank (that would kill `gofastr build` too).
func warnMissingMarkdown() {
	miss := agentsinv.MissingMarkdown()
	if len(miss) == 0 {
		return
	}
	fmt.Println()
	info("Warning: %d entr%s registered without agents.md content:", len(miss), pluralY(len(miss)))
	for _, e := range miss {
		info("  - %s:%s (%s) — check //go:embed agents.md", e.Kind, e.Name, e.ImportPath)
	}
}

func pluralY(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}

// buildAgentsMD assembles a full AGENTS.md from preamble + auto section
// + tail. Used by `agents init`.
func buildAgentsMD() []byte {
	var b bytes.Buffer
	b.WriteString(agentsPreamble)
	b.WriteString("\n")
	b.WriteString(agentsAutoStart)
	b.WriteString("\n\n")
	writeAutoSection(&b)
	b.WriteString(agentsAutoEnd)
	b.WriteString("\n")
	b.WriteString(agentsTail)
	return b.Bytes()
}

// refreshAgentsMD swaps the content between the two markers in
// existing, leaving everything else untouched. Returns the new bytes
// and a boolean indicating whether anything changed.
//
// Each marker must occur EXACTLY ONCE — a user who quotes the marker
// literals inside the preamble (in an explanatory paragraph, say)
// would otherwise have bytes.Index return their prose-mention, and
// the regenerated content would clobber the user's intro.
func refreshAgentsMD(existing []byte) ([]byte, bool, error) {
	startCount := bytes.Count(existing, []byte(agentsAutoStart))
	endCount := bytes.Count(existing, []byte(agentsAutoEnd))
	if startCount == 0 || endCount == 0 {
		return nil, false, fmt.Errorf("missing %q / %q marker(s) — was the file edited by hand without preserving them?", agentsAutoStart, agentsAutoEnd)
	}
	if startCount != 1 || endCount != 1 {
		return nil, false, fmt.Errorf("expected exactly one occurrence of each marker (saw %d %q / %d %q); refusing to refresh", startCount, agentsAutoStart, endCount, agentsAutoEnd)
	}
	startIdx := bytes.Index(existing, []byte(agentsAutoStart))
	endIdx := bytes.Index(existing, []byte(agentsAutoEnd))
	if endIdx < startIdx {
		return nil, false, fmt.Errorf("end marker %q appears before start marker %q; refusing to refresh", agentsAutoEnd, agentsAutoStart)
	}
	var b bytes.Buffer
	b.Write(existing[:startIdx+len(agentsAutoStart)])
	b.WriteString("\n\n")
	writeAutoSection(&b)
	b.Write(existing[endIdx:])
	out := b.Bytes()
	return out, !bytes.Equal(normalizeMD(out), normalizeMD(existing)), nil
}

// normalizeMD collapses runs of whitespace so the changed-check ignores
// trailing newline jitter. Also folds CRLF → LF so Windows clones
// (autocrlf=true) don't flap on every sync.
var wsRun = regexp.MustCompile(`[ \t]+\n`)

func normalizeMD(b []byte) []byte {
	b = bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))
	return wsRun.ReplaceAll(b, []byte("\n"))
}

// writeAutoSection appends a TOC table linking every inventory entry
// to its detail file under agents/. The detail files themselves are
// written by writeAgentDetailFiles — keeping AGENTS.md thin so an AI
// agent can scan it in one pass and only load the detail when a row
// matches its task.
func writeAutoSection(b *bytes.Buffer) {
	entries := agentsinv.All()
	if len(entries) == 0 {
		b.WriteString("_No inventory entries registered._\n\n")
		return
	}
	b.WriteString("| Section | Use this when | Details |\n")
	b.WriteString("|---|---|---|\n")
	for _, e := range entries {
		title := sectionTitle(e)
		usewhen := extractUseWhen(e.Markdown)
		path := agentDetailRelPath(e)
		fmt.Fprintf(b, "| **%s** | %s | [%s](%s) |\n", title, usewhen, path, path)
	}
	b.WriteString("\n")
}

// sectionTitle is "framework" for framework entries, "battery/<name>"
// for battery entries. Matches how the existing agents.md files start.
func sectionTitle(e agentsinv.Entry) string {
	if e.Kind == agentsinv.KindBattery {
		return "battery/" + e.Name
	}
	return e.Name
}

// agentDetailRelPath is the path to the per-entry detail file, relative
// to the AGENTS.md that links to it (both live at project root).
func agentDetailRelPath(e agentsinv.Entry) string {
	if e.Kind == agentsinv.KindBattery {
		return "agents/battery-" + e.Name + ".md"
	}
	return "agents/" + e.Name + ".md"
}

// extractUseWhen pulls the **Use this when** paragraph out of a
// battery's agents.md so the TOC row can show its full trigger-phrase
// list. The paragraph extends until EITHER a blank line OR the next
// `**Heading:**` on a fresh line — markdown paragraphs canonically
// end on a blank line, but a battery author who writes
// `**Use this when** foo\n**Import:** pkg` (no blank line) shouldn't
// have the trigger list slurp the **Import:** block too.
//
// Returns "—" when no such paragraph exists.
func extractUseWhen(md string) string {
	const marker = "**Use this when**"
	start := strings.Index(md, marker)
	if start < 0 {
		return "—"
	}
	// Walk lines from the marker forward, accumulating until we hit
	// a paragraph terminator.
	rest := md[start+len(marker):]
	lines := strings.Split(rest, "\n")
	var collected []string
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Blank line ends the paragraph.
		if i > 0 && trimmed == "" {
			break
		}
		// A fresh **...:** heading on the next line ends the paragraph
		// even without a blank separator.
		if i > 0 && reBoldHeading.MatchString(trimmed) {
			break
		}
		collected = append(collected, trimmed)
	}
	s := strings.Join(collected, " ")
	s = strings.Join(strings.Fields(s), " ")
	s = strings.TrimPrefix(s, "the prompt mentions: ")
	s = strings.TrimPrefix(s, "the prompt mentions:")
	s = strings.TrimSuffix(s, ".")
	// Markdown table cells can't contain a literal pipe — escape any.
	s = strings.ReplaceAll(s, "|", `\|`)
	if s == "" {
		return "—"
	}
	return s
}

// reBoldHeading matches a line that BEGINS with `**Word:**` — the
// next-heading terminator used by extractUseWhen.
var reBoldHeading = regexp.MustCompile(`^\*\*[A-Za-z][^*]*:\*\*`)

// autoGenSentinel is the string we look for at the top of an existing
// detail file to decide it's safe to overwrite. Present ⇒ previous
// generator output; absent ⇒ user-authored file we must not clobber.
const autoGenSentinel = "<!-- AUTO-GENERATED by `gofastr agents sync`"

// writeAgentDetailFiles writes one Markdown file per inventory entry
// under <dir>/agents/. Each file leads with an HTML comment that
// announces it's auto-generated AND names the upstream source, so a
// user who finds it knows where to edit instead of clobbering the
// local copy. Body = the entry's agents.md verbatim, followed by a
// back-link to AGENTS.md so an agent that lands here can pivot to
// the index.
//
// Refuses to overwrite an existing detail-file path that does NOT
// carry the auto-gen sentinel — that would silently clobber a user
// file (e.g. a hand-written `agents/framework.md` they had before
// running init). Other files in agents/ are left untouched.
func writeAgentDetailFiles(dir string) error {
	target := filepath.Join(dir, "agents")
	if err := os.MkdirAll(target, 0o755); err != nil {
		return fmt.Errorf("create agents dir: %w", err)
	}
	for _, e := range agentsinv.All() {
		name := agentDetailRelPath(e) // "agents/battery-admin.md" etc.
		path := filepath.Join(dir, name)

		// Refuse to clobber a non-generated file at the same path.
		if existing, err := os.ReadFile(path); err == nil {
			if !bytes.HasPrefix(existing, []byte(autoGenSentinel)) {
				return fmt.Errorf("refusing to overwrite %s — it lacks the AUTO-GENERATED header, so it looks hand-written. Move or delete it first, then re-run", path)
			}
		}

		var b bytes.Buffer
		fmt.Fprintf(&b,
			"%s — edits to this file will be lost on next sync. Canonical source: %s/agents.md -->\n\n",
			autoGenSentinel, e.ImportPath)
		b.WriteString(strings.TrimRight(e.Markdown, "\n"))
		b.WriteString("\n\n---\n\n")
		b.WriteString("← Back to [AGENTS.md](../AGENTS.md)\n")
		if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}
	return nil
}

const agentsPreamble = `# AGENTS.md

> **For AI agents (Claude Code / Cursor / Aider / anything else):**
> Read this file FIRST before writing any code in this project. It
> exists to keep you from reinventing primitives the framework
> already provides.

## Don't reinvent — index

Scan the table below. When your task matches a **Use this when**
phrase, open the linked detail file under ` + "`agents/`" + ` for the
full ` + "`Use this when` / `Import` / `Shape` / `Don't reinvent`" + `
breakdown — short enough to load on demand, kept in sync by
` + "`gofastr agents sync`" + `.

The table between the markers below is auto-generated; every detail
file under ` + "`agents/`" + ` is too. Edit everything OUTSIDE the
markers (and outside ` + "`agents/`" + `) freely.
`

const agentsTail = `

## Project conventions

> Edit this section. Document app-specific rules an agent should
> follow (PHI hygiene, env vars, schemas it must not touch, your
> failure posture). Anything below this heading is preserved by
> ` + "`gofastr agents sync`" + `.

`
