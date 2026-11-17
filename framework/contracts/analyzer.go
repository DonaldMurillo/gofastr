package contracts

import (
	"fmt"
	"maps"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// Analyzer is one detector: a name, the rules it may emit, and a Run.
type Analyzer struct {
	// Name is the stable identifier used by `--analyzer` and in timings.
	Name string
	// Doc is one line describing what the analyzer looks at.
	Doc string
	// Rules are the IDs this analyzer may emit. [Run] drops a diagnostic
	// naming any other rule and reports it as an analyzer error.
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

// Register adds analyzers to the process-wide set. It panics on a missing
// name or Run, no rules, a duplicate name, or a rule the catalog lacks.
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
	return slices.SortedFunc(maps.Values(analyzers), func(a, b *Analyzer) int {
		return strings.Compare(a.Name, b.Name)
	})
}

// RunOptions narrows a run. An empty filter means everything.
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

// AnalyzerTiming records how long one analyzer took and what it found.
type AnalyzerTiming struct {
	Name        string        `json:"name"`
	Duration    time.Duration `json:"-"`
	Millis      float64       `json:"ms"`
	Diagnostics int           `json:"diagnostics"`
	Error       string        `json:"error,omitempty"`
}

// Run executes the selected analyzers against the pass and assembles a
// [Report]: diagnostics normalized against the catalog, relaxed by config,
// filtered by suppression, deduplicated, and sorted. It errors when no
// analyzer is registered, the pass found no files or could not read some,
// or an unknown analyzer name is requested.
func Run(p *Pass, opts RunOptions) (*Report, error) {
	if p == nil {
		return nil, fmt.Errorf("contracts: nil pass")
	}
	if len(Analyzers()) == 0 {
		return nil, fmt.Errorf(`contracts: no analyzers registered: import _ "github.com/DonaldMurillo/gofastr/framework/contracts/analyzers" in the binary that calls Run`)
	}
	if len(p.files) == 0 {
		return nil, fmt.Errorf("contracts: no analysable source files discovered under %q", p.Root)
	}
	if err := p.discoveryError(); err != nil {
		return nil, err
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
				return nil, fmt.Errorf("unknown analyzer %q: run `gofastr verify --list` to see the catalog", name)
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

	// Two passes: the first consumes GOFASTR0002 waivers, the second
	// reports the stale directives that survive.
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
