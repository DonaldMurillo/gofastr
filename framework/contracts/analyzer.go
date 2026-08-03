package contracts

import (
	"fmt"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// Analyzer is one detector. It is a struct rather than an interface for
// the same reason golang.org/x/tools/go/analysis uses one: the interesting
// part is the data (which rules can this emit), and a struct keeps that
// declaration next to the function instead of scattered across methods.
type Analyzer struct {
	// Name is the stable identifier used by `--analyzer` and in timings.
	Name string
	// Doc is one line describing what the analyzer looks at.
	Doc string
	// Rules are the IDs this analyzer may emit. [Run] rejects a
	// diagnostic naming any other rule — an analyzer cannot smuggle in an
	// undocumented finding, which is what keeps the catalog honest.
	Rules []string
	// Run inspects the pass. Returning an error aborts only this
	// analyzer; the rest of the run continues and the error is reported.
	Run func(*Pass) ([]Diagnostic, error)
}

// Capabilities are the distinct capabilities this analyzer's rules cover.
func (a *Analyzer) Capabilities() []Capability {
	seen := map[Capability]bool{}
	var out []Capability
	for _, id := range a.Rules {
		if r, ok := LookupRule(id); ok && !seen[r.Capability] {
			seen[r.Capability] = true
			out = append(out, r.Capability)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Order() < out[j].Order() })
	return out
}

var (
	analyzerMu sync.RWMutex
	analyzers  = map[string]*Analyzer{}
)

// Register adds analyzers to the process-wide set. Panics on a duplicate
// name or a rule the catalog does not know — both are wiring errors that
// belong at init, not in a user's terminal.
func Register(as ...*Analyzer) {
	analyzerMu.Lock()
	defer analyzerMu.Unlock()
	for _, a := range as {
		if a == nil || a.Name == "" {
			panic("contracts: analyzer needs a name")
		}
		if a.Run == nil {
			panic("contracts: analyzer " + a.Name + " has no Run")
		}
		if len(a.Rules) == 0 {
			panic("contracts: analyzer " + a.Name + " declares no rules")
		}
		if _, dup := analyzers[a.Name]; dup {
			panic("contracts: duplicate analyzer " + a.Name)
		}
		for _, id := range a.Rules {
			if _, ok := LookupRule(id); !ok {
				panic(fmt.Sprintf("contracts: analyzer %s declares unknown rule %s", a.Name, id))
			}
		}
		analyzers[a.Name] = a
	}
}

// Analyzers returns every registered analyzer, name-sorted.
func Analyzers() []*Analyzer {
	analyzerMu.RLock()
	defer analyzerMu.RUnlock()
	out := make([]*Analyzer, 0, len(analyzers))
	for _, a := range analyzers {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// RunOptions narrows a run. Both filters are additive-empty: an empty
// slice means "everything", which is the strict default.
type RunOptions struct {
	// Capabilities restricts the run to analyzers covering at least one
	// of these. Empty runs them all.
	Capabilities []Capability
	// Analyzers restricts the run by analyzer name. Empty runs them all.
	Analyzers []string
	// Parallel caps concurrent analyzers. Zero uses GOMAXPROCS.
	Parallel int
}

func (o RunOptions) wants(a *Analyzer) bool {
	if len(o.Analyzers) > 0 {
		found := false
		for _, name := range o.Analyzers {
			if name == a.Name {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if len(o.Capabilities) == 0 {
		return true
	}
	for _, want := range o.Capabilities {
		for _, have := range a.Capabilities() {
			if have == want {
				return true
			}
		}
	}
	return false
}

// AnalyzerTiming records how long one analyzer took, for `--json` output
// and for finding the analyzer that made the pipeline slow.
type AnalyzerTiming struct {
	Name        string        `json:"name"`
	Duration    time.Duration `json:"-"`
	Millis      float64       `json:"ms"`
	Diagnostics int           `json:"diagnostics"`
	Error       string        `json:"error,omitempty"`
}

// Run executes the selected analyzers against the pass and assembles a
// [Report]: diagnostics normalized against the catalog, relaxed by config,
// filtered by suppression, deduplicated, and sorted.
func Run(p *Pass, opts RunOptions) (*Report, error) {
	if p == nil {
		return nil, fmt.Errorf("contracts: nil pass")
	}
	// Analyzers self-register from an init() in
	// framework/contracts/analyzers, so a binary that never imports that
	// package has an empty registry. Running zero analyzers produces zero
	// diagnostics, which is indistinguishable from a clean tree — the
	// worst possible failure for a tool whose whole job is saying "this
	// is fine". Refuse instead.
	if len(Analyzers()) == 0 {
		return nil, fmt.Errorf(`contracts: no analyzers registered — import _ "github.com/DonaldMurillo/gofastr/framework/contracts/analyzers" in the binary that calls Run`)
	}
	selected := make([]*Analyzer, 0)
	for _, a := range Analyzers() {
		if opts.wants(a) {
			selected = append(selected, a)
		}
	}
	if len(opts.Analyzers) > 0 {
		known := map[string]bool{}
		for _, a := range Analyzers() {
			known[a.Name] = true
		}
		for _, name := range opts.Analyzers {
			if !known[name] {
				return nil, fmt.Errorf("unknown analyzer %q — run `gofastr verify --list` to see the catalog", name)
			}
		}
	}

	start := time.Now()
	raw, timings := runAll(p, selected, opts.Parallel)

	report := &Report{
		Root:         p.Root,
		ConfigPath:   p.Config.Path,
		Unparsed:     len(p.Unparsed()),
		Capabilities: opts.Capabilities,
		Timings:      timings,
		FailOn:       p.Config.FailOn,
		Relaxations:  p.Config.Relaxations(),
	}
	for _, t := range timings {
		if t.Error != "" {
			report.Errors = append(report.Errors, fmt.Sprintf("%s: %s", t.Name, t.Error))
		}
	}

	sup := collectSuppressions(p)
	raw = append(raw, sup.issues...)

	// active decides which rules this run actually evaluated. A stale
	// suppression is only reportable for a rule that ran — otherwise
	// `gofastr verify routing` would flag every accessibility suppression
	// in the tree as dead.
	active := func(r Rule) bool {
		if !p.Config.Enabled(r) {
			return false
		}
		for _, a := range selected {
			for _, id := range a.Rules {
				if id == r.ID {
					return true
				}
			}
		}
		return r.Capability == CapMeta
	}

	kept := make([]Diagnostic, 0, len(raw))
	for _, d := range raw {
		rule, ok := LookupRule(d.RuleID)
		if !ok {
			report.Errors = append(report.Errors,
				fmt.Sprintf("diagnostic references unknown rule %q at %s", d.RuleID, d.Location()))
			continue
		}
		sev := p.Config.SeverityFor(rule)
		if sev == SeverityOff {
			continue
		}
		if p.Config.ExemptFor(rule, d.File) {
			continue
		}
		if sup.suppressed(d, rule) {
			// Every suppressed finding keeps its (rule, file) identity so
			// the baseline can treat its slot as occupied. Deliberately
			// NOT filtered by the current fail floor: the baseline being
			// applied may have been recorded under a different one (a
			// --strict lane records warn entries a non-strict run still
			// applies), and a slot is a slot — an unclaimed one absorbs
			// the next new finding whatever severity it carries. Claims
			// for rules the baseline never recorded are simply no-ops.
			report.noteSuppressed(rule.ID, d.File)
			continue
		}
		d.Slug = rule.Slug
		d.Capability = rule.Capability
		d.Severity = sev
		if d.Suggestion == "" {
			d.Suggestion = rule.Fix
		}
		if d.Snippet == "" && d.Line > 0 && !d.RedactSnippet {
			d.Snippet = p.Line(d.File, d.Line)
		}
		ruleCopy := rule
		d.Rule = &ruleCopy
		kept = append(kept, d)
	}

	// Stale suppressions are computed after the filtering loop, when every
	// directive that matched something has been marked used. Their
	// findings then go through the SAME contract as every other finding —
	// exemptions apply, and a `//gofastr:allow(GOFASTR0002)` waives one.
	// Skipping those checks made the meta rule the one rule in the
	// catalog that could not be waived locally, with the waiver itself
	// reported stale. Two passes because consuming a waiver marks that
	// directive used: the first pass consumes, the recompute drops the
	// now-used waivers from the candidate set, and the second pass emits
	// what survives.
	//
	// Consumption is deliberately eager — before the staleReportable
	// check — so a waiver is marked used even on a capability-narrowed
	// run where its stale finding would not have been reportable. The
	// cost is a Suppressed counter that can include those waivers; the
	// alternative leaves the waiver unused and reports IT stale on the
	// next full run, which is worse than an over-count.
	for _, d := range sup.stale(p) {
		if rule, ok := LookupRule(d.RuleID); ok && p.Config.Enabled(rule) && !p.Config.ExemptFor(rule, d.File) {
			sup.suppressed(d, rule)
		}
	}
	for _, d := range sup.stale(p) {
		rule, _ := LookupRule(d.RuleID)
		if !p.Config.Enabled(rule) {
			continue
		}
		if p.Config.ExemptFor(rule, d.File) {
			continue
		}
		if sup.suppressed(d, rule) {
			report.noteSuppressed(rule.ID, d.File)
			continue
		}
		if !staleReportable(sup, d, active) {
			continue
		}
		d.Slug, d.Capability, d.Severity = rule.Slug, rule.Capability, p.Config.SeverityFor(rule)
		ruleCopy := rule
		d.Rule = &ruleCopy
		kept = append(kept, d)
	}

	kept = dedupe(kept)
	sortDiagnostics(kept)
	report.Diagnostics = kept
	report.Duration = time.Since(start)
	report.summarize()
	return report, nil
}

// staleReportable keeps a stale-suppression finding only when every rule
// the directive names was actually evaluated this run.
func staleReportable(sup *suppressionSet, d Diagnostic, active func(Rule) bool) bool {
	for _, s := range sup.byFile[d.File] {
		if s.Line != d.Line || s.used {
			continue
		}
		for _, name := range s.Rules {
			r, ok := LookupRule(name)
			if !ok || !active(r) {
				return false
			}
		}
	}
	return true
}

// runAll executes analyzers concurrently. The pass is safe for concurrent
// use (its caches are mutex-guarded), and analyzers are pure readers, so
// the only shared mutable state is the result slice.
func runAll(p *Pass, selected []*Analyzer, parallel int) ([]Diagnostic, []AnalyzerTiming) {
	if parallel <= 0 {
		parallel = runtime.GOMAXPROCS(0)
	}
	if parallel > len(selected) {
		parallel = len(selected)
	}
	if parallel < 1 {
		parallel = 1
	}

	type result struct {
		idx    int
		diags  []Diagnostic
		timing AnalyzerTiming
	}
	results := make([]result, len(selected))
	sem := make(chan struct{}, parallel)
	var wg sync.WaitGroup

	for i, a := range selected {
		wg.Add(1)
		go func(i int, a *Analyzer) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			// An analyzer panic must not take the whole verify run with
			// it: report it as that analyzer's error and let the other
			// eleven finish. A half-report beats no report.
			defer func() {
				if rec := recover(); rec != nil {
					results[i] = result{idx: i, timing: AnalyzerTiming{
						Name:  a.Name,
						Error: fmt.Sprintf("panic: %v", rec),
					}}
				}
			}()
			started := time.Now()
			diags, err := a.Run(p)
			t := AnalyzerTiming{
				Name:        a.Name,
				Duration:    time.Since(started),
				Diagnostics: len(diags),
			}
			t.Millis = float64(t.Duration.Microseconds()) / 1000
			if err != nil {
				t.Error = err.Error()
			}
			// Guard the catalog contract: an analyzer may only emit rules
			// it declared. Anything else is dropped with a loud error
			// rather than silently trusted.
			declared := map[string]bool{}
			for _, id := range a.Rules {
				declared[id] = true
			}
			filtered := diags[:0]
			for _, d := range diags {
				if !declared[d.RuleID] {
					t.Error = strings.TrimSpace(t.Error + fmt.Sprintf(
						" emitted undeclared rule %s at %s;", d.RuleID, d.Location()))
					continue
				}
				filtered = append(filtered, d)
			}
			results[i] = result{idx: i, diags: filtered, timing: t}
		}(i, a)
	}
	wg.Wait()

	var all []Diagnostic
	timings := make([]AnalyzerTiming, 0, len(selected))
	for _, r := range results {
		all = append(all, r.diags...)
		if r.timing.Name != "" {
			timings = append(timings, r.timing)
		}
	}
	sort.Slice(timings, func(i, j int) bool { return timings[i].Duration > timings[j].Duration })
	return all, timings
}
