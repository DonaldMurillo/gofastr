package agentscmd

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	_ "github.com/DonaldMurillo/gofastr/battery/admin"
	_ "github.com/DonaldMurillo/gofastr/battery/auth"
	_ "github.com/DonaldMurillo/gofastr/battery/cache"
	_ "github.com/DonaldMurillo/gofastr/battery/desktop"
	_ "github.com/DonaldMurillo/gofastr/battery/email"
	_ "github.com/DonaldMurillo/gofastr/battery/log"
	_ "github.com/DonaldMurillo/gofastr/battery/notify"
	_ "github.com/DonaldMurillo/gofastr/battery/print"
	_ "github.com/DonaldMurillo/gofastr/battery/queue"
	_ "github.com/DonaldMurillo/gofastr/battery/relay"
	_ "github.com/DonaldMurillo/gofastr/battery/rtc"
	_ "github.com/DonaldMurillo/gofastr/battery/search"
	_ "github.com/DonaldMurillo/gofastr/battery/service"
	_ "github.com/DonaldMurillo/gofastr/battery/setup"
	_ "github.com/DonaldMurillo/gofastr/battery/sitesearch"
	_ "github.com/DonaldMurillo/gofastr/battery/storage"
	_ "github.com/DonaldMurillo/gofastr/battery/telemetry"
	_ "github.com/DonaldMurillo/gofastr/battery/webhook"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	_ "github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/agentsinv"
	_ "github.com/DonaldMurillo/gofastr/framework/headless"
	_ "github.com/DonaldMurillo/gofastr/framework/localentity"
	_ "github.com/DonaldMurillo/gofastr/framework/ui"
)

const (
	agentsAutoStart = "<!-- gofastr-batteries:start -->"
	agentsAutoEnd   = "<!-- gofastr-batteries:end -->"
)

// RunAgents dispatches the `gofastr agents <subcommand>` family.
func RunAgents(args []string) {
	if cli.HasHelpFlag(args) {
		printAgentsUsage()
		return
	}
	if len(args) == 0 {
		cli.UsageFail("Subcommand required. Usage: gofastr agents [init|sync|skill] [dir]")
	}
	switch args[0] {
	case "init":
		runAgentsInit(args[1:])
	case "sync":
		runAgentsSync(args[1:])
	case "skill":
		runAgentsSkill(args[1:])
	default:
		cli.UsageFail("Unknown subcommand %q. Try `gofastr agents init`, `gofastr agents sync`, or `gofastr agents skill`.", args[0])
	}
}

func agentsDirArg(args []string) string {
	switch {
	case len(args) == 0:
		return "."
	case len(args) > 1:
		cli.Fail("Unexpected argument %q: an agents verb takes at most one directory.", args[1])
	case strings.HasPrefix(args[0], "-"):
		cli.Fail("Unknown flag %q.", args[0])
	default:
		return args[0]
	}
	printAgentsUsage()
	cli.OsExit(2)
	return ""
}

func runAgentsInit(args []string) {
	dir := agentsDirArg(args)
	target := filepath.Join(dir, "AGENTS.md")
	if err := createAgentsMD(dir); err != nil {
		if errors.Is(err, fs.ErrExist) {
			cli.Fail("%s already exists. Use `gofastr agents sync` to refresh.", target)
		} else {
			cli.Fail("Failed to write %s: %v", target, err)
		}
		cli.OsExit(1)
		return
	}
	if err := WriteAgentDetailFiles(dir); err != nil {
		cli.Fail("Failed to write .agents/ details: %v", err)
		cli.OsExit(1)
	}
	cli.Success("Wrote %s and %d detail file(s) under .agents/", target, len(agentsinv.All()))
	warnMissingMarkdown()
}

func createAgentsMD(dir string) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	f, err := root.OpenFile("AGENTS.md", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(BuildAgentsMD()); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func runAgentsSkill(args []string) {
	force := false
	var dirArgs []string
	for _, a := range args {
		if a == "--force" {
			force = true
			continue
		}
		dirArgs = append(dirArgs, a)
	}
	dir := agentsDirArg(dirArgs)
	if err := WriteHostSkill(dir, force); err != nil {
		cli.Fail("Failed to write skill: %v", err)
		cli.OsExit(1)
	}
	cli.Success("Wrote %s/.claude/skills/gofastr-host/SKILL.md", dir)
}

func runAgentsSync(args []string) {
	dir := agentsDirArg(args)
	target := filepath.Join(dir, "AGENTS.md")
	root, err := os.OpenRoot(dir)
	if err != nil {
		cli.Fail("Cannot read %s: %v", target, err)
		cli.Info("Run `gofastr agents init` first.")
		cli.OsExit(1)
		return
	}
	defer root.Close()
	existing, err := readInRoot(root, "AGENTS.md")
	if err != nil {
		cli.Fail("Cannot read %s: %v", target, err)
		cli.Info("Run `gofastr agents init` first.")
		cli.OsExit(1)
		return
	}
	refreshed, changed, err := RefreshAgentsMD(existing)
	if err != nil {
		cli.Fail("Refresh failed: %v", err)
		cli.OsExit(1)
	}
	if err := WriteAgentDetailFiles(dir); err != nil {
		cli.Fail("Failed to write .agents/ details: %v", err)
		cli.OsExit(1)
	}
	if !changed {
		cli.Info("%s TOC already up to date (.agents/ details refreshed).", target)
		warnMissingMarkdown()
		return
	}
	if err := replaceInRoot(root, "AGENTS.md", refreshed, 0o644); err != nil {
		cli.Fail("Failed to write %s: %v", target, err)
		cli.OsExit(1)
	}
	cli.Success("Refreshed %s and %d detail file(s) under .agents/", target, len(agentsinv.All()))
	warnMissingMarkdown()
}

func warnMissingMarkdown() {
	miss := agentsinv.MissingMarkdown()
	if len(miss) == 0 {
		return
	}
	fmt.Println()
	cli.Info("Warning: %d entr%s registered without agents.md content:", len(miss), pluralY(len(miss)))
	for _, e := range miss {
		cli.Info("  - %s:%s (%s): check //go:embed agents.md", e.Kind, e.Name, e.ImportPath)
	}
}

func pluralY(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}

// BuildAgentsMD assembles a fresh AGENTS.md: preamble, the generated table
// between the markers, and tail.
func BuildAgentsMD() []byte {
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

// RefreshAgentsMD replaces the text between the markers in existing and
// reports whether anything changed. It errors unless each marker occurs
// exactly once, start before end.
func RefreshAgentsMD(existing []byte) ([]byte, bool, error) {
	startCount := bytes.Count(existing, []byte(agentsAutoStart))
	endCount := bytes.Count(existing, []byte(agentsAutoEnd))
	if startCount == 0 || endCount == 0 {
		return nil, false, fmt.Errorf("missing %q / %q marker(s): was the file edited by hand without preserving them?", agentsAutoStart, agentsAutoEnd)
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
	b.Write(migrateLegacyAgentsRefs(existing[:startIdx+len(agentsAutoStart)]))
	b.WriteString("\n\n")
	writeAutoSection(&b)
	b.Write(migrateLegacyAgentsRefs(existing[endIdx:]))
	out := b.Bytes()
	return out, !bytes.Equal(NormalizeMD(out), NormalizeMD(existing)), nil
}

func migrateLegacyAgentsRefs(b []byte) []byte {
	b = bytes.ReplaceAll(b, []byte("`"+LegacyAgentsDetailDir+"/"), []byte("`"+AgentsDetailDir+"/"))
	return bytes.ReplaceAll(b, []byte("]("+LegacyAgentsDetailDir+"/"), []byte("]("+AgentsDetailDir+"/"))
}

var wsRun = regexp.MustCompile(`[ \t]+\n`)

// NormalizeMD folds CRLF to LF and drops trailing blanks on each line, so a
// compare ignores both.
func NormalizeMD(b []byte) []byte {
	b = bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))
	return wsRun.ReplaceAll(b, []byte("\n"))
}

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

func sectionTitle(e agentsinv.Entry) string {
	if e.Kind == agentsinv.KindBattery {
		return "battery/" + e.Name
	}
	return e.Name
}

func agentDetailRelPath(e agentsinv.Entry) string {
	if e.Kind == agentsinv.KindBattery {
		return ".agents/battery-" + e.Name + ".md"
	}
	return ".agents/" + e.Name + ".md"
}

func extractUseWhen(md string) string {
	const marker = "**Use this when**"
	start := strings.Index(md, marker)
	if start < 0 {
		return "—"
	}
	rest := md[start+len(marker):]
	lines := strings.Split(rest, "\n")
	var collected []string
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if i > 0 && trimmed == "" {
			break
		}
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
	s = strings.ReplaceAll(s, "|", `\|`)
	if s == "" {
		return "—"
	}
	return s
}

var reBoldHeading = regexp.MustCompile(`^\*\*[A-Za-z][^*]*:\*\*`)

const autoGenSentinel = "<!-- AUTO-GENERATED by `gofastr agents sync`"

// CheckOnboardingFiles errors when a detail file or the host skill in dir
// is hand-written, unreadable or linked out of dir. It writes nothing.
func CheckOnboardingFiles(dir string) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, entry := range agentsinv.All() {
		if err := checkAgentDetailOverwrite(root, dir, agentDetailRelPath(entry)); err != nil {
			return err
		}
	}
	if _, err := readInRoot(root, hostSkillDir+"/SKILL.md"); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("refusing to overwrite host skill: %w", err)
	}
	return nil
}

func checkAgentDetailOverwrite(root *os.Root, dir, name string) error {
	existing, err := readInRoot(root, name)
	if err == nil {
		if !bytes.HasPrefix(existing, []byte(autoGenSentinel)) {
			return fmt.Errorf("refusing to overwrite %s: it lacks the AUTO-GENERATED header, so it looks hand-written. Move or delete it first, then re-run", filepath.Join(dir, name))
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("refusing to overwrite %s: %w", filepath.Join(dir, name), err)
	}
	return nil
}

// WriteAgentDetailFiles writes one generated file per inventory entry under
// dir/.agents/, refuses to overwrite one without the generated header, and
// removes the generated files an older release left in agents/.
func WriteAgentDetailFiles(dir string) error {
	root, err := openProjectRoot(dir)
	if err != nil {
		return fmt.Errorf("create %s dir: %w", AgentsDetailDir, err)
	}
	defer root.Close()
	if err := root.MkdirAll(AgentsDetailDir, 0o755); err != nil {
		return fmt.Errorf("create %s dir: %w", AgentsDetailDir, err)
	}
	for _, e := range agentsinv.All() {
		name := agentDetailRelPath(e)
		path := filepath.Join(dir, name)

		if err := checkAgentDetailOverwrite(root, dir, name); err != nil {
			return err
		}

		var b bytes.Buffer
		fmt.Fprintf(&b,
			"%s: edits to this file will be lost on next sync. Canonical source: %s/agents.md -->\n\n",
			autoGenSentinel, e.ImportPath)
		b.WriteString(strings.TrimRight(e.Markdown, "\n"))
		b.WriteString("\n\n---\n\n")
		b.WriteString("← Back to [AGENTS.md](../AGENTS.md)\n")
		if err := replaceInRoot(root, name, b.Bytes(), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}
	return removeLegacyAgentDetailFiles(root)
}

// AgentsDetailDir holds the generated detail files; releases before it
// wrote them to LegacyAgentsDetailDir.
const (
	AgentsDetailDir       = ".agents"
	LegacyAgentsDetailDir = "agents"
)

func removeLegacyAgentDetailFiles(root *os.Root) error {
	fi, err := root.Lstat(LegacyAgentsDetailDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("stat %s: %w", LegacyAgentsDetailDir, err)
	}
	if !fi.IsDir() {
		return nil
	}
	d, err := root.Open(LegacyAgentsDetailDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("open %s: %w", LegacyAgentsDetailDir, err)
	}
	entries, err := d.ReadDir(-1)
	d.Close()
	if err != nil {
		return fmt.Errorf("read %s: %w", LegacyAgentsDetailDir, err)
	}
	removed := 0
	for _, ent := range entries {
		if !ent.Type().IsRegular() || !strings.HasSuffix(ent.Name(), ".md") {
			continue
		}
		name := path.Join(LegacyAgentsDetailDir, ent.Name())
		body, err := root.ReadFile(name)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		if !bytes.HasPrefix(body, []byte(autoGenSentinel)) {
			continue
		}
		if err := root.Remove(name); err != nil {
			return fmt.Errorf("remove %s: %w", name, err)
		}
		removed++
	}
	if removed == len(entries) {
		if err := root.Remove(LegacyAgentsDetailDir); err != nil {
			return fmt.Errorf("remove %s: %w", LegacyAgentsDetailDir, err)
		}
	}
	return nil
}

const agentsPreamble = `# AGENTS.md

> **For AI agents (Claude Code / Cursor / Aider / anything else):**
> Read this file FIRST before writing any code in this project. It
> exists to keep you from reinventing primitives the framework
> already provides.

## Run the app while you work

` + DevLoopGuidance + `
## Two maps, then the detail

Each is one page and cheaper than the topic docs it routes into. Land on the
row for your job first; open a topic only when a row points you at one.

- ` + "`" + `gofastr docs backend-capability-map` + "`" + `: data, auth, APIs, jobs,
  deploys. Every row names the symbols to compose and a command that
  proves the result.
- ` + "`" + `gofastr docs ui-capability-map` + "`" + `: screens, state ownership, and
  delivery.

## Secure by default

Entity CRUD requires a signed-in session until you opt out: ` + "`" + `Public: true` + "`" + `
on the entity's Exposure opens it to anonymous requests; wire battery/auth
for a real login flow (` + "`" + `gofastr docs auth` + "`" + `).

## UI starts with product direction

Before selecting UI components, complete or refresh ` + "`" + `DESIGN.md` + "`" + `: primary
task, density, dominant element, composition model, mobile priority, and two
patterns to avoid. Then read ` + "`" + `.agents/ui.md` + "`" + ` and
` + "`" + `gofastr docs ui-composition-recipes` + "`" + `; use the recipe's named primitives rather
than approximating its shape with Box/Card/Stack. For a dominant record or
operational state, start with RecordSummary + MetricBand, keep actions in an
Actions slot or Cluster, and do not repeat the same state in a Banner. Keep a
RecordSummary description to one or two sentences; use its compact Aside for
supporting owner/presence context and MetricBand hints for trends. Give a long
product name a short phone wordmark in the app's header package. On wide detail routes, pair
related bounded modules such as DetailLists in Grid rather than leaving a
narrow stacked column beside an accidental empty rail; reflow the pair on
phones. Cluster wraps whole controls by default; reserve ClusterConfig.NoWrap
for compact chrome guaranteed to fit. Visual review at desktop and mobile is
part of completion, not an optional polish pass.

## Don't reinvent index

Scan the table below. When your task matches a **Use this when**
phrase, open the linked detail file under ` + "`.agents/`" + ` for the
full ` + "`Use this when` / `Import` / `Shape` / `Don't reinvent`" + `
breakdown: short enough to load on demand, kept in sync by
` + "`gofastr agents sync`" + `.

The table between the markers below is auto-generated; every detail
file under ` + "`.agents/`" + ` is too. Edit everything OUTSIDE the
markers (and outside ` + "`.agents/`" + `) freely.
`

const agentsTail = `
## Project conventions

> Edit this section. Document app-specific rules an agent should
> follow (PHI hygiene, env vars, schemas it must not touch, your
> failure posture). Anything below this heading is preserved by
> ` + "`gofastr agents sync`" + `.

`
