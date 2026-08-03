package contracts

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// BaselineFileName is the conventional location for a recorded baseline.
// It lives in the repository, not under `.gofastr/`: unlike the coverage
// manifest this is a reviewed decision about what debt is accepted, and
// it belongs in the diff where a reader can see it shrink.
const BaselineFileName = ".gofastr-contracts-baseline.json"

// BaselineSchemaVersion guards the on-disk shape.
const BaselineSchemaVersion = 1

// Baseline is the debt an existing codebase has agreed to carry.
//
// It exists because strict-by-default and adoption pull against each
// other. A mature app switching `gofastr verify` on gets hundreds of
// findings at once; nobody fixes hundreds of findings at once, so the
// realistic outcomes are "turn the tool off" or "downgrade every rule to
// warn", and both end with nothing being enforced. A baseline gives the
// third option: accept what is there, fail on what is added.
//
// Counts are keyed by (rule, file) rather than by line, because line
// numbers churn on every edit and a baseline that goes stale on a
// reformat is a baseline people delete. Moving a finding within a file
// keeps it accepted; adding one more of the same rule to that file does
// not.
type Baseline struct {
	Schema int `json:"schema"`
	// Generated is when the baseline was recorded, for the report's "this
	// is N months old" nudge.
	Generated string `json:"generated"`
	// Note is free text explaining why this debt is accepted.
	Note string `json:"note,omitempty"`
	// Counts maps rule ID → file → number of accepted findings.
	Counts map[string]map[string]int `json:"counts"`
}

// NewBaseline records the report's *gating* diagnostics as accepted.
//
// Findings below the run's fail-on severity are deliberately left out. A
// baseline exists to unblock a gate; recording something that cannot fail
// the run is not just noise in the file, it is actively harmful — the
// entry absorbs the finding on every later run, so an informational
// signal the project wanted to keep watching disappears instead.
//
// This matters for the semantic-coverage rules in particular. They are
// environment-dependent (they record which tests ran), so a project will
// often downgrade them to info rather than let them gate; without this
// filter, `--baseline-write` would then silence the very findings the
// downgrade was meant to keep visible.
func NewBaseline(r *Report, now time.Time, note string) *Baseline {
	b := &Baseline{
		Schema:    BaselineSchemaVersion,
		Generated: now.UTC().Format(time.RFC3339),
		Note:      note,
		Counts:    map[string]map[string]int{},
	}
	for _, d := range r.Diagnostics {
		if d.Severity < r.FailOn {
			continue
		}
		if b.Counts[d.RuleID] == nil {
			b.Counts[d.RuleID] = map[string]int{}
		}
		b.Counts[d.RuleID][d.File]++
	}
	return b
}

// Total is the number of accepted findings across every rule and file.
func (b *Baseline) Total() int {
	if b == nil {
		return 0
	}
	n := 0
	for _, files := range b.Counts {
		for _, c := range files {
			n += c
		}
	}
	return n
}

// WriteBaseline saves a baseline as indented JSON, sorted so a
// regenerated file diffs cleanly against the previous one.
func WriteBaseline(path string, b *Baseline) error {
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return fmt.Errorf("encode baseline: %w", err)
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write baseline: %w", err)
	}
	return nil
}

// ReadBaseline loads a baseline. A missing file returns (nil, nil) — no
// baseline is the normal state, not an error.
func ReadBaseline(path string) (*Baseline, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read baseline %s: %w", path, err)
	}
	var b Baseline
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("parse baseline %s: %w", path, err)
	}
	if b.Schema > BaselineSchemaVersion {
		return nil, fmt.Errorf("baseline %s is version %d, this build understands %d — re-record it",
			path, b.Schema, BaselineSchemaVersion)
	}
	if b.Counts == nil {
		b.Counts = map[string]map[string]int{}
	}
	return &b, nil
}

// BaselineResult is what applying a baseline did to a report.
type BaselineResult struct {
	// Accepted is how many findings the baseline absorbed.
	Accepted int
	// Fixed lists (rule, file) pairs where fewer findings occur now than
	// the baseline records — debt that was paid down. Reported so the
	// baseline visibly shrinks instead of quietly over-accepting forever.
	Fixed []BaselineDelta
}

// BaselineDelta is one (rule, file) whose accepted count is now too high.
type BaselineDelta struct {
	RuleID   string
	File     string
	Baseline int
	Current  int
}

// ApplyBaseline removes baselined findings from the report and returns
// what it absorbed.
//
// Diagnostics are dropped worst-first within each (rule, file) bucket, so
// when a file has three accepted findings and gains a fourth, the one
// left visible is the most severe — not whichever happened to sort last.
func (r *Report) ApplyBaseline(b *Baseline) BaselineResult {
	var res BaselineResult
	if b == nil || len(b.Counts) == 0 {
		return res
	}
	// One-shot: the apply consumes the diagnostic list, so a second call
	// would see it already emptied, re-claim the suppressed slots into
	// the Fixed deltas, and rewrite Baselined to 0. No caller does this
	// on purpose — which is why the misuse is inert instead of silently
	// corrupting the counters.
	if r.baselineApplied {
		return res
	}
	r.baselineApplied = true

	remaining := make(map[string]map[string]int, len(b.Counts))
	for rule, files := range b.Counts {
		remaining[rule] = make(map[string]int, len(files))
		for file, n := range files {
			remaining[rule][file] = n
		}
	}

	// A suppressed finding still occupies its slot. Suppression is
	// consumed inside Run, so by the time the baseline is applied the
	// finding is invisible — but it has not gone away, and handing its
	// allowance to the next finding of the same rule in the same file
	// would let debt grow behind a balanced count. Claimed slots are not
	// Accepted (nothing visible was absorbed); they surface below as
	// over-accepting, because a re-recorded baseline — which also runs
	// after suppression — would no longer include them.
	claimed := map[string]map[string]int{}
	for rule, files := range r.suppressedAt {
		rem, ok := remaining[rule]
		if !ok {
			continue
		}
		for file, n := range files {
			take := n
			if take > rem[file] {
				take = rem[file]
			}
			if take <= 0 {
				continue
			}
			rem[file] -= take
			if claimed[rule] == nil {
				claimed[rule] = map[string]int{}
			}
			claimed[rule][file] = take
		}
	}

	kept := r.Diagnostics[:0]
	for _, d := range r.Diagnostics {
		if files, ok := remaining[d.RuleID]; ok && files[d.File] > 0 {
			files[d.File]--
			res.Accepted++
			continue
		}
		kept = append(kept, d)
	}
	r.Diagnostics = kept
	r.Baselined = res.Accepted

	// Whatever is left unconsumed is debt that has been paid — and a
	// suppression-claimed slot counts as unconsumed here, because the
	// re-recorded baseline this nudge asks for would drop it too.
	for rule, files := range remaining {
		for file, left := range files {
			left += claimed[rule][file]
			if left <= 0 {
				continue
			}
			res.Fixed = append(res.Fixed, BaselineDelta{
				RuleID: rule, File: file,
				Baseline: b.Counts[rule][file],
				Current:  b.Counts[rule][file] - left,
			})
		}
	}
	sort.Slice(res.Fixed, func(i, j int) bool {
		if res.Fixed[i].File != res.Fixed[j].File {
			return res.Fixed[i].File < res.Fixed[j].File
		}
		return res.Fixed[i].RuleID < res.Fixed[j].RuleID
	})
	r.BaselineFixed = len(res.Fixed)
	r.summarize()
	return res
}
