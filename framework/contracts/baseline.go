package contracts

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/atomicfile"
)

// BaselineFileName is the conventional location for a recorded baseline,
// at the project root.
const BaselineFileName = ".gofastr-contracts-baseline.json"

// BaselineSchemaVersion guards the on-disk shape; a newer file is refused.
const BaselineSchemaVersion = 1

// Baseline is the debt an existing codebase has agreed to carry, counted
// per (rule, file): a finding may move within its file, one more fails.
type Baseline struct {
	Schema int `json:"schema"`
	// Generated is when the baseline was recorded, RFC 3339 UTC.
	Generated string `json:"generated"`
	// Note is free text explaining why this debt is accepted.
	Note string `json:"note,omitempty"`
	// Counts maps rule ID → file → number of accepted findings.
	Counts map[string]map[string]int `json:"counts"`
}

// NewBaseline records the report's diagnostics at or above its fail-on
// severity as accepted; findings below it are left out.
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

// WriteBaseline saves a baseline atomically as indented JSON with sorted
// keys, creating its directory.
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
	if err := atomicfile.WriteFile(path, append(data, '\n'), atomicfile.Mode(0o644)); err != nil {
		return fmt.Errorf("write baseline: %w", err)
	}
	return nil
}

// ReadBaseline loads a baseline. A missing file returns (nil, nil); a
// newer schema is an error.
func ReadBaseline(path string) (*Baseline, error) {
	data, err := atomicfile.ReadFile(path)
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
		return nil, fmt.Errorf("baseline %s is version %d, this build understands %d: re-record it",
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
	// the baseline records.
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
// what it absorbed. It drops worst-first within each (rule, file), counts
// a suppressed finding against its slot, and acts only once per report.
func (r *Report) ApplyBaseline(b *Baseline) BaselineResult {
	var res BaselineResult
	if b == nil || len(b.Counts) == 0 {
		return res
	}
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
