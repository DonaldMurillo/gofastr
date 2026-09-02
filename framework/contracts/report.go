package contracts

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// CapabilitySummary is the per-capability tally shown in the report
// footer, the "which area of this app is drifting" view.
type CapabilitySummary struct {
	Capability Capability `json:"capability"`
	Errors     int        `json:"errors"`
	Warnings   int        `json:"warnings"`
	Infos      int        `json:"infos"`
}

// Total is every diagnostic in this capability.
func (c CapabilitySummary) Total() int { return c.Errors + c.Warnings + c.Infos }

// VetResult records what the `go vet` stage did. It is set by the CLI,
// not by [Run], vet is a pipeline stage around the analyzers rather than
// one of them, but it rides on the report because the report is what
// gets serialised, and a consumer that cannot tell whether the code even
// compiles is reading the analyzers' findings without their precondition.
type VetResult struct {
	// Ran is false when the stage was skipped.
	Ran bool `json:"ran"`
	// Passed is meaningful only when Ran, and the wire format enforces
	// that: see MarshalJSON.
	Passed bool `json:"passed"`
	// Skipped explains why the stage did not run, e.g. "--no-vet".
	Skipped string `json:"skipped,omitempty"`
	// Output is vet's diagnostic text when it failed.
	Output string `json:"output,omitempty"`
}

// MarshalJSON omits the verdict when the stage never ran. A skipped
// stage serialising `"passed": false` is "we did not check" reading as
// "it failed": a consumer keying on vet.passed alone would fail every
// --no-vet run. The plain omitempty cannot express this (it would also
// drop a genuine ran-and-failed false), so the shaping is explicit.
func (v *VetResult) MarshalJSON() ([]byte, error) {
	type wire struct {
		Ran     bool   `json:"ran"`
		Passed  *bool  `json:"passed,omitempty"`
		Skipped string `json:"skipped,omitempty"`
		Output  string `json:"output,omitempty"`
	}
	w := wire{Ran: v.Ran, Skipped: v.Skipped, Output: v.Output}
	if v.Ran {
		w.Passed = &v.Passed
	}
	return json.Marshal(w)
}

// FixedDiagnostic is one applied autofix, in the wire format.
type FixedDiagnostic struct {
	Rule string `json:"rule"`
	File string `json:"file"`
	Line int    `json:"line"`
}

// Report is the outcome of a verify run.
type Report struct {
	// Vet records the `go vet` stage, when the caller ran one.
	Vet *VetResult `json:"vet,omitempty"`
	// Root is the absolute directory analysed.
	Root string `json:"root"`
	// ConfigPath is the config file used, "" when defaults applied.
	ConfigPath string `json:"config,omitempty"`
	// Capabilities is the requested filter, empty when everything ran.
	Capabilities []Capability `json:"capabilities,omitempty"`
	// Diagnostics is every surviving finding, worst first.
	Diagnostics []Diagnostic `json:"diagnostics"`
	// Suppressed counts findings silenced by a `//gofastr:allow`
	// directive. Reported so a clean run still admits what it skipped.
	Suppressed int `json:"suppressed"`
	// suppressedAt keys the suppressed *gating* findings by rule then
	// file. [Report.ApplyBaseline] needs the identities, not merely the
	// count: a suppressed finding still occupies its baseline slot, and
	// without knowing where the suppressions landed, adding an allow
	// directive to baselined debt would free that slot to absorb the next
	// NEW finding of the rule in the file, growth hidden behind a
	// balanced count. Unexported: the JSON document already admits the
	// total via Suppressed, and narrowed copies drop it with the other
	// run-wide counters.
	suppressedAt map[string]map[string]int
	// baselineApplied makes [Report.ApplyBaseline] one-shot; see there.
	baselineApplied bool
	// Baselined counts findings absorbed by a recorded baseline:
	// pre-existing debt the project agreed to carry.
	Baselined int `json:"baselined,omitempty"`
	// BaselineFixed counts (rule, file) buckets whose baseline allowance
	// is now larger than the findings that remain. Debt that was paid
	// down and should be re-recorded so the baseline keeps shrinking.
	BaselineFixed int `json:"baselineFixed,omitempty"`
	// Notices are human-facing remarks the CLI would otherwise print to
	// stdout: "this rule has no autofix", "not a git repository". In text
	// mode they are printed; in JSON they belong here, because anything
	// printed alongside the document corrupts it, and dropping them
	// silently would lose the run's own account of what it did.
	Notices []string `json:"notices,omitempty"`
	// Fixed lists the diagnostics --fix resolved, so a JSON consumer
	// driving verify → fix → verify can see what changed.
	Fixed []FixedDiagnostic `json:"fixed,omitempty"`
	// Unparsed counts files the parser rejected. Those files produced no
	// findings from any analyzer, so without this a mid-edit tree reads as
	// clean for exactly the files nobody could read.
	Unparsed int `json:"unparsed,omitempty"`
	// OutsideChange counts findings dropped by --changed because they sit
	// in files this change did not touch. Reported so a narrowed run
	// never reads as a whole-repository all-clear.
	OutsideChange int `json:"outsideChange,omitempty"`
	// Errors are analyzer failures: a broken analyzer, not a broken app.
	Errors []string `json:"analyzerErrors,omitempty"`
	// Timings is per-analyzer wall time, slowest first.
	Timings []AnalyzerTiming `json:"timings,omitempty"`
	// Relaxations lists every configured downgrade, so `verify` cannot
	// pass quietly on a config that turned the interesting half off.
	Relaxations []string `json:"relaxations,omitempty"`
	// FailOn is the severity floor that decides the exit code.
	FailOn Severity `json:"failOn"`
	// Duration is the whole run's wall time.
	Duration time.Duration `json:"-"`

	// Summary is the per-capability tally, in capability order.
	Summary []CapabilitySummary `json:"summary"`
	// Counts are the run-wide totals.
	Counts struct {
		Errors   int `json:"errors"`
		Warnings int `json:"warnings"`
		Infos    int `json:"infos"`
	} `json:"counts"`
}

func (r *Report) summarize() {
	byCap := map[Capability]*CapabilitySummary{}
	r.Counts.Errors, r.Counts.Warnings, r.Counts.Infos = 0, 0, 0
	for _, d := range r.Diagnostics {
		s, ok := byCap[d.Capability]
		if !ok {
			s = &CapabilitySummary{Capability: d.Capability}
			byCap[d.Capability] = s
		}
		switch d.Severity {
		case SeverityError:
			s.Errors++
			r.Counts.Errors++
		case SeverityWarn:
			s.Warnings++
			r.Counts.Warnings++
		default:
			s.Infos++
			r.Counts.Infos++
		}
	}
	r.Summary = r.Summary[:0]
	for _, s := range byCap {
		r.Summary = append(r.Summary, *s)
	}
	sort.Slice(r.Summary, func(i, j int) bool {
		return r.Summary[i].Capability.Order() < r.Summary[j].Capability.Order()
	})
}

// Passed reports whether the run should be treated as a success. An
// analyzer error fails the run: a check that could not execute has proven
// nothing, and treating "didn't run" as "passed" is how a gate silently
// stops gating.
// noteSuppressed counts one suppressed finding and records its (rule,
// file) identity for the baseline's slot-claiming. One helper for every
// suppression site on purpose: the stale-directive pass had its own
// branch that incremented the counter and skipped the recording, and a
// waived stale directive's freed slot absorbed brand-new stale debt.
func (r *Report) noteSuppressed(ruleID, file string) {
	r.Suppressed++
	if r.suppressedAt == nil {
		r.suppressedAt = map[string]map[string]int{}
	}
	if r.suppressedAt[ruleID] == nil {
		r.suppressedAt[ruleID] = map[string]int{}
	}
	r.suppressedAt[ruleID][file]++
}

func (r *Report) Passed() bool {
	if len(r.Errors) > 0 {
		return false
	}
	// A failed vet stage is the same case: the analyzers ran against code
	// the compiler rejects, so an empty diagnostic list means "we could
	// not look", not "there was nothing to find".
	if r.Vet != nil && r.Vet.Ran && !r.Vet.Passed {
		return false
	}
	for _, d := range r.Diagnostics {
		if d.Severity >= r.FailOn && d.Severity != SeverityOff {
			return false
		}
	}
	return true
}

// ExitCode is the process status for a CLI wrapping this report: 0 clean,
// 1 findings at or above the fail-on floor.
func (r *Report) ExitCode() int {
	if r.Passed() {
		return 0
	}
	return 1
}

// Fixable returns the diagnostics carrying a mechanical fix.
func (r *Report) Fixable() []Diagnostic {
	var out []Diagnostic
	for _, d := range r.Diagnostics {
		if d.Fix != nil && len(d.Fix.Edits) > 0 {
			out = append(out, d)
		}
	}
	return out
}

// Only returns a shallow copy of the report holding just the diagnostics
// for the given rules, accepted as IDs or slugs. It exists so a caller can
// fix one rule at a time: Apply writes every fix in the report, which is
// the wrong granularity when an agent has decided to accept one rule's
// edits and review another's by hand.
//
// The counters (Suppressed, Baselined, …) are deliberately NOT carried
// over: they describe the whole run, and copying them onto a narrowed
// report would state that this rule alone silenced that many findings.
func (r *Report) Only(rules ...string) *Report {
	want := make(map[string]bool, len(rules))
	var unknown []string
	for _, name := range rules {
		if rule, ok := LookupRule(name); ok {
			want[rule.ID] = true
			continue
		}
		// Unknown: match nothing rather than everything, the safe
		// default for a fixer, but never silently. A typo'd name
		// narrowing to an empty, passing scope is the library-caller
		// trap; recording it as an analyzer error keeps the "a check
		// that could not execute has proven nothing" contract.
		unknown = append(unknown, name)
	}
	out := narrowedShell(r)
	for _, name := range unknown {
		out.Errors = append(out.Errors,
			fmt.Sprintf("narrowed to unknown rule %q: nothing matched; run `gofastr verify --list` for the catalog", name))
	}
	for _, d := range r.Diagnostics {
		if want[d.RuleID] {
			out.Diagnostics = append(out.Diagnostics, d)
		}
	}
	// Summary, Counts and therefore Passed are derived from Diagnostics.
	// Leaving them at zero would print a narrowed report's findings under
	// a "0 errors" footer, and report Passed on a run that just failed.
	out.summarize()
	return out
}

// narrowedShell is the run-wide state every narrowed copy must keep.
//
// The split is between facts about the RUN and counts about the
// DIAGNOSTICS. Analyzer errors, unparsed files, relaxations, the vet
// stage and the capability filter all describe what this run did and did
// not manage to check, and they stay true however the findings are
// filtered. Dropping Errors made `--rule` exit 0 after an analyzer
// panicked, which is precisely the "a check that could not execute has
// proven nothing" case [Report.Passed] exists to catch.
//
// Suppressed, Baselined, BaselineFixed and OutsideChange are deliberately
// NOT carried: each counts diagnostics across the whole run, and copying
// them onto a narrowed report would claim this rule alone accounted for
// them.
func narrowedShell(r *Report) *Report {
	out := &Report{
		Root:         r.Root,
		ConfigPath:   r.ConfigPath,
		FailOn:       r.FailOn,
		Vet:          r.Vet,
		Unparsed:     r.Unparsed,
		Capabilities: r.Capabilities,
		Notices:      r.Notices,
		Fixed:        r.Fixed,
	}
	out.Errors = append(out.Errors, r.Errors...)
	out.Relaxations = append(out.Relaxations, r.Relaxations...)
	out.Timings = append(out.Timings, r.Timings...)
	// The one-shot marker travels too: a narrowed copy of a report whose
	// baseline was already applied must not accept a second application:
	// the copy has no suppression identities, so it would re-absorb.
	out.baselineApplied = r.baselineApplied
	return out
}

// OnlyFiles returns a shallow copy of the report holding just the
// diagnostics in the given file set. Unlike [Report.RestrictTo] it does
// not mutate the receiver, which is what makes it usable for narrowing a
// fix without also narrowing the report that gets printed.
//
// A nil set returns a copy holding everything: "no restriction" and
// "restrict to nothing" are different requests, and conflating them would
// make an unfiltered run silently fix nothing.
func (r *Report) OnlyFiles(files map[string]bool) *Report {
	out := narrowedShell(r)
	for _, d := range r.Diagnostics {
		if files == nil || (d.File != "" && files[d.File]) {
			out.Diagnostics = append(out.Diagnostics, d)
		}
	}
	out.summarize()
	return out
}

// containedPath resolves rel against root and refuses anything that lands
// outside it. Returns the absolute path to write. Containment is checked
// BOTH lexically and on the symlink-resolved paths: a file inside the
// tree that is a symlink pointing outside passes the lexical check, and
// writing through it would edit the target instead.
func containedPath(root, rel string) (string, error) {
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("apply fixes to %s: absolute paths are not applied", rel)
	}
	abs := filepath.Join(root, filepath.FromSlash(rel))
	base := filepath.Clean(root)
	if abs != base && !strings.HasPrefix(abs, base+string(filepath.Separator)) {
		return "", fmt.Errorf("apply fixes to %s: resolves outside the project root", rel)
	}
	realAbs, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("apply fixes to %s: %w", rel, err)
	}
	realRoot, err := filepath.EvalSymlinks(base)
	if err != nil {
		return "", fmt.Errorf("apply fixes: resolve root: %w", err)
	}
	if realAbs != realRoot && !strings.HasPrefix(realAbs, realRoot+string(filepath.Separator)) {
		return "", fmt.Errorf("apply fixes to %s: symlink resolves outside the project root", rel)
	}
	return abs, nil
}

// Apply writes every suggested fix in the report to disk and returns the
// diagnostics it resolved.
//
// Edits are byte offsets captured when the file was read, so Apply
// re-reads each file and refuses to write when an edit no longer fits:
// out-of-range offsets always, and for edits that record their expected
// text (TextEdit.Old), any file whose content at those offsets changed
// since analysis. A stale offset silently applied is a corrupted source
// file, which is a far worse outcome than "run verify again".
func (r *Report) Apply() ([]Diagnostic, error) {
	byFile := map[string][]Diagnostic{}
	for _, d := range r.Fixable() {
		byFile[d.File] = append(byFile[d.File], d)
	}
	files := make([]string, 0, len(byFile))
	for f := range byFile {
		files = append(files, f)
	}
	sort.Strings(files)

	var applied []Diagnostic
	for _, rel := range files {
		// filepath.Join cleans `..` segments away, so a diagnostic whose
		// File escaped the root would be written to silently. Every path
		// here comes from the pass's own walk today, but this is the one
		// function in the package that writes to disk, and since
		// contracts_fix exposes it over MCP, containment is checked here
		// rather than trusted from the caller.
		abs, err := containedPath(r.Root, rel)
		if err != nil {
			return applied, err
		}
		body, err := os.ReadFile(abs)
		if err != nil {
			return applied, fmt.Errorf("apply fixes to %s: %w", rel, err)
		}
		var edits []TextEdit
		var owners []Diagnostic
		for _, d := range byFile[rel] {
			for _, e := range d.Fix.Edits {
				if e.Start < 0 || e.End < e.Start || e.End > len(body) {
					return applied, fmt.Errorf("apply fixes to %s: edit is out of range: the file changed since analysis, re-run verify", rel)
				}
				if e.Old != "" && string(body[e.Start:e.End]) != e.Old {
					return applied, fmt.Errorf("apply fixes to %s: the text at %d–%d is not what the analyzer saw: the file changed since analysis, re-run verify", rel, e.Start, e.End)
				}
				edits = append(edits, e)
			}
			owners = append(owners, d)
		}
		// Apply back-to-front so earlier offsets stay valid, and reject
		// overlaps rather than resolving them by luck.
		sort.Slice(edits, func(i, j int) bool { return edits[i].Start > edits[j].Start })
		prevStart := len(body) + 1
		out := body
		for _, e := range edits {
			if e.End > prevStart {
				return applied, fmt.Errorf("apply fixes to %s: two fixes overlap: apply one at a time", rel)
			}
			merged := make([]byte, 0, len(out)-(e.End-e.Start)+len(e.New))
			merged = append(merged, out[:e.Start]...)
			merged = append(merged, e.New...)
			merged = append(merged, out[e.End:]...)
			out = merged
			prevStart = e.Start
		}
		// Re-format Go output. This is what lets a fix insert fields into
		// a composite literal without having to reproduce the surrounding
		// indentation exactly, the edit only has to be *correct*, not
		// pretty.
		if strings.HasSuffix(rel, ".go") {
			if formatted, ferr := format.Source(out); ferr == nil {
				// gofmt always emits LF. Restoring the file's original
				// convention matters on Windows: without it, a one-line
				// fix in a CRLF file rewrites every line in the file, and
				// an autofixer whose diffs are 400 lines for a 1-line
				// change is one nobody runs twice.
				if bytes.Contains(body, []byte("\r\n")) {
					formatted = bytes.ReplaceAll(formatted, []byte("\n"), []byte("\r\n"))
				}
				out = formatted
			} else if _, origErr := format.Source(body); origErr == nil {
				// The input parsed and the edited result does not: the
				// edit is what broke it, which means it landed somewhere
				// it did not belong: an Old span short enough to match a
				// coincidental byte (a brace, say) passes the content
				// check and still writes into the wrong construct after
				// the file moved underneath the report. Refuse; writing
				// known-invalid Go and reporting success is corruption.
				return applied, fmt.Errorf("apply fixes to %s: the edit produces Go that no longer parses: the file changed since analysis, re-run verify", rel)
			}
			// An input that was ALREADY unparsable keeps the raw edit:
			// the tree may be mid-refactor, and silently discarding the
			// fix would be worse than leaving it unformatted.
		}
		info, err := os.Stat(abs)
		mode := os.FileMode(0o644)
		if err == nil {
			mode = info.Mode().Perm()
		}
		if err := os.WriteFile(abs, out, mode); err != nil {
			return applied, fmt.Errorf("write %s: %w", rel, err)
		}
		applied = append(applied, owners...)
	}
	return applied, nil
}
