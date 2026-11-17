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

// CapabilitySummary is the per-capability tally shown in the report footer.
type CapabilitySummary struct {
	Capability Capability `json:"capability"`
	Errors     int        `json:"errors"`
	Warnings   int        `json:"warnings"`
	Infos      int        `json:"infos"`
}

// Total is every diagnostic in this capability.
func (c CapabilitySummary) Total() int { return c.Errors + c.Warnings + c.Infos }

// VetResult records what the `go vet` stage did. The CLI sets it, not [Run].
type VetResult struct {
	// Ran is false when the stage was skipped.
	Ran bool `json:"ran"`
	// Passed is meaningful only when Ran.
	Passed bool `json:"passed"`
	// Skipped explains why the stage did not run, e.g. "--no-vet".
	Skipped string `json:"skipped,omitempty"`
	// Output is vet's diagnostic text when it failed.
	Output string `json:"output,omitempty"`
	// SuiteSkipped says why GoFastr's analyzers did not run beside vet
	// (a module on a newer Go than the CLI) when vet itself ran.
	SuiteSkipped string `json:"suiteSkipped,omitempty"`
}

// MarshalJSON omits "passed" when the stage never ran, and writes it,
// false included, when it did.
func (v *VetResult) MarshalJSON() ([]byte, error) {
	type wire struct {
		Ran     bool   `json:"ran"`
		Passed  *bool  `json:"passed,omitempty"`
		Skipped string `json:"skipped,omitempty"`
		Output  string `json:"output,omitempty"`
		Suite   string `json:"suiteSkipped,omitempty"`
	}
	w := wire{Ran: v.Ran, Skipped: v.Skipped, Output: v.Output, Suite: v.SuiteSkipped}
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
	// Suppressed counts findings silenced by a `//gofastr:allow` directive.
	Suppressed int `json:"suppressed"`

	suppressedAt    map[string]map[string]int
	baselineApplied bool
	// Baselined counts findings absorbed by a recorded baseline:
	// pre-existing debt the project agreed to carry.
	Baselined int `json:"baselined,omitempty"`
	// BaselineFixed counts (rule, file) buckets whose baseline allowance
	// is now larger than the findings that remain.
	BaselineFixed int `json:"baselineFixed,omitempty"`
	// Notices are human-facing remarks ("not a git repository") that JSON
	// mode carries here instead of printing.
	Notices []string `json:"notices,omitempty"`
	// Fixed lists the diagnostics --fix resolved.
	Fixed []FixedDiagnostic `json:"fixed,omitempty"`
	// Unparsed counts files reading or parsing rejected; nonzero fails the run.
	Unparsed int `json:"unparsed,omitempty"`
	// OutsideChange counts findings --changed dropped as outside the change.
	OutsideChange int `json:"outsideChange,omitempty"`
	// Errors are analyzer failures; any one fails the run.
	Errors []string `json:"analyzerErrors,omitempty"`
	// Timings is per-analyzer wall time, slowest first.
	Timings []AnalyzerTiming `json:"timings,omitempty"`
	// Relaxations lists every configured severity change and exemption.
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

// Passed reports whether the run is a success: no analyzer error, no
// unparsed file, no failed vet stage, and no finding at or above FailOn.
func (r *Report) Passed() bool {
	if len(r.Errors) > 0 || r.Unparsed > 0 {
		return false
	}
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
// for the given rules, as IDs or slugs, with its tallies recomputed. An
// unknown name matches nothing and is recorded as an analyzer error.
// Run-wide facts are kept; the Suppressed, Baselined, BaselineFixed and
// OutsideChange counters are not.
func (r *Report) Only(rules ...string) *Report {
	want := make(map[string]bool, len(rules))
	var unknown []string
	for _, name := range rules {
		if rule, ok := LookupRule(name); ok {
			want[rule.ID] = true
			continue
		}
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
	out.summarize()
	return out
}

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
	out.baselineApplied = r.baselineApplied
	return out
}

// OnlyFiles is [Report.Only] by file set, without mutating the receiver
// as [Report.RestrictTo] does. A nil set keeps every diagnostic.
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

func localPath(rel string) (string, error) {
	name := filepath.FromSlash(rel)
	if filepath.IsAbs(name) || strings.HasPrefix(rel, "/") {
		return "", fmt.Errorf("apply fixes to %s: absolute paths are not applied", rel)
	}
	if !filepath.IsLocal(name) {
		return "", fmt.Errorf("apply fixes to %s: resolves outside the project root", rel)
	}
	return filepath.Clean(name), nil
}

// Apply writes every suggested fix in the report to disk, through an
// os.Root at the report root, and returns the diagnostics it resolved. It
// refuses a path outside the root, an out-of-range edit, an edit whose Old
// text no longer matches, overlapping edits, and a Go edit that breaks a
// file that parsed. Go output is gofmt'd, keeping CRLF line endings.
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
	if len(files) == 0 {
		return applied, nil
	}
	rt, err := os.OpenRoot(r.Root)
	if err != nil {
		return applied, fmt.Errorf("apply fixes: open root: %w", err)
	}
	defer rt.Close()
	for _, rel := range files {
		name, err := localPath(rel)
		if err != nil {
			return applied, err
		}
		body, err := rt.ReadFile(name)
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
		if strings.HasSuffix(rel, ".go") {
			if formatted, ferr := format.Source(out); ferr == nil {
				if bytes.Contains(body, []byte("\r\n")) {
					formatted = bytes.ReplaceAll(formatted, []byte("\n"), []byte("\r\n"))
				}
				out = formatted
			} else if _, origErr := format.Source(body); origErr == nil {
				return applied, fmt.Errorf("apply fixes to %s: the edit produces Go that no longer parses: the file changed since analysis, re-run verify", rel)
			}
		}
		info, err := rt.Stat(name)
		mode := os.FileMode(0o644)
		if err == nil {
			mode = info.Mode().Perm()
		}
		if err := rt.WriteFile(name, out, mode); err != nil {
			return applied, fmt.Errorf("write %s: %w", rel, err)
		}
		applied = append(applied, owners...)
	}
	return applied, nil
}
