package contracts

import (
	"fmt"
	"sort"
	"strings"
)

// TextOptions tunes the human report.
type TextOptions struct {
	// Color emits ANSI escapes. Callers set this from a TTY check.
	Color bool
	// Verbose prints the rule's Why and an example under every finding
	// rather than only under the first of each rule. The compact default
	// exists because twenty instances of one rule should teach the lesson
	// once, not twenty times.
	Verbose bool
	// Timings appends the per-analyzer wall times.
	Timings bool
}

// FormatText renders a report for a terminal. The shape is deliberate:
// findings grouped by rule rather than by file, because the unit of
// *action* is the rule, you learn it once and then fix every instance,
// while a file-grouped list makes the reader re-derive the lesson at each
// stop.
func FormatText(r *Report, opts TextOptions) string {
	var b strings.Builder
	c := colorizer{on: opts.Color}

	if len(r.Diagnostics) == 0 {
		fmt.Fprintf(&b, "  %s %s\n", c.green("✓"), "Contracts verified: no findings.")
		writeQuietFooter(&b, r, c)
		if opts.Timings {
			writeTimings(&b, r, c)
		}
		return b.String()
	}

	// Group by rule, preserving the sorted order of first appearance so
	// errors still lead.
	order := []string{}
	byRule := map[string][]Diagnostic{}
	for _, d := range r.Diagnostics {
		if _, seen := byRule[d.RuleID]; !seen {
			order = append(order, d.RuleID)
		}
		byRule[d.RuleID] = append(byRule[d.RuleID], d)
	}

	for _, id := range order {
		ds := byRule[id]
		head := ds[0]
		rule := head.Rule
		title, why, fix, doc := id, "", "", ""
		if rule != nil {
			title = rule.Title
			why, fix, doc = rule.Why, rule.Fix, rule.DocCommand()
		}

		fmt.Fprintf(&b, "\n%s %s  %s\n",
			c.severity(head.Severity), c.bold(id), title)
		fmt.Fprintf(&b, "  %s\n", c.dim(string(head.Capability)+" · "+head.Slug))

		for _, d := range ds {
			fmt.Fprintf(&b, "\n  %s\n", c.bold(sanitizeText(d.Location())))
			fmt.Fprintf(&b, "    %s\n", sanitizeText(d.Message))
			if d.Snippet != "" {
				fmt.Fprintf(&b, "    %s %s\n", c.dim("│"), c.dim(sanitizeText(truncate(d.Snippet, 110))))
			}
			if d.Suggestion != "" && (opts.Verbose || d.Suggestion != fix) {
				fmt.Fprintf(&b, "    %s %s\n", c.cyan("fix:"), sanitizeText(d.Suggestion))
			}
			if d.Fix != nil {
				fmt.Fprintf(&b, "    %s %s\n", c.green("autofix:"), d.Fix.Description)
			}
		}

		if why != "" {
			fmt.Fprintf(&b, "\n  %s %s\n", c.dim("why:"), wrap(why, 76, "       "))
		}
		if fix != "" {
			fmt.Fprintf(&b, "  %s %s\n", c.cyan("fix:"), wrap(fix, 76, "       "))
		}
		if rule != nil && len(rule.Examples) > 0 && (opts.Verbose || len(ds) > 1) {
			writeExample(&b, c, rule.Examples[0])
		}
		if doc != "" {
			fmt.Fprintf(&b, "  %s %s\n", c.dim("docs:"), doc)
		}
	}

	b.WriteString("\n")
	writeSummary(&b, r, c)
	writeQuietFooter(&b, r, c)
	if opts.Timings {
		writeTimings(&b, r, c)
	}
	return b.String()
}

func writeExample(b *strings.Builder, c colorizer, ex Example) {
	if ex.Caption != "" {
		fmt.Fprintf(b, "  %s %s\n", c.dim("example:"), ex.Caption)
	}
	for _, line := range strings.Split(ex.Bad, "\n") {
		fmt.Fprintf(b, "       %s %s\n", c.red("✗"), line)
	}
	for _, line := range strings.Split(ex.Good, "\n") {
		fmt.Fprintf(b, "       %s %s\n", c.green("✓"), line)
	}
}

func writeSummary(b *strings.Builder, r *Report, c colorizer) {
	fmt.Fprintf(b, "  %s\n", c.bold("Summary"))
	width := 0
	for _, s := range r.Summary {
		if n := len(s.Capability.Title()); n > width {
			width = n
		}
	}
	for _, s := range r.Summary {
		parts := []string{}
		if s.Errors > 0 {
			parts = append(parts, c.red(fmt.Sprintf("%d error", s.Errors))+plural(s.Errors))
		}
		if s.Warnings > 0 {
			parts = append(parts, c.yellow(fmt.Sprintf("%d warning", s.Warnings))+plural(s.Warnings))
		}
		if s.Infos > 0 {
			parts = append(parts, c.dim(fmt.Sprintf("%d info", s.Infos)))
		}
		fmt.Fprintf(b, "    %-*s  %s\n", width, s.Capability.Title(), strings.Join(parts, ", "))
	}
}

// writeQuietFooter states what the run did not check. A verify that
// passes because half of it was switched off should say so: the whole
// point of "no hidden behavior" is that a clean report is trustworthy.
func writeQuietFooter(b *strings.Builder, r *Report, c colorizer) {
	if r.Suppressed > 0 {
		fmt.Fprintf(b, "\n  %s %d finding%s suppressed by //gofastr:allow directives\n",
			c.dim("·"), r.Suppressed, plural(r.Suppressed))
	}
	if r.Baselined > 0 {
		fmt.Fprintf(b, "  %s %d pre-existing finding%s absorbed by the baseline\n",
			c.dim("·"), r.Baselined, plural(r.Baselined))
	}
	if r.BaselineFixed > 0 {
		// The ratchet: debt that was paid stays recorded as accepted
		// until someone re-records, which would let new findings hide in
		// the slack. Saying so is what keeps the baseline shrinking.
		fmt.Fprintf(b, "  %s %d baseline entr%s now over-accepting: re-record with %s\n",
			c.green("·"), r.BaselineFixed,
			map[bool]string{true: "y is", false: "ies are"}[r.BaselineFixed == 1],
			c.bold("gofastr verify --baseline-write"))
	}
	// A file the parser rejected produced no findings from any analyzer.
	// Saying so is the difference between "these files are clean" and
	// "nobody could read these files", mid-edit that is expected, in CI
	// it is the whole story.
	if r.Unparsed > 0 {
		fmt.Fprintf(b, "  %s %d file%s could not be parsed and %s not checked\n",
			c.yellow("·"), r.Unparsed, plural(r.Unparsed),
			map[bool]string{true: "was", false: "were"}[r.Unparsed == 1])
	}
	if r.OutsideChange > 0 {
		// A narrowed run must never read as a whole-repository all-clear.
		fmt.Fprintf(b, "  %s %d finding%s outside the changed files: run without %s for the full picture\n",
			c.yellow("·"), r.OutsideChange, plural(r.OutsideChange), c.bold("--changed"))
	}
	if len(r.Relaxations) > 0 {
		fmt.Fprintf(b, "  %s config relaxations in effect:\n", c.yellow("·"))
		for _, rel := range r.Relaxations {
			fmt.Fprintf(b, "      %s\n", c.dim(rel))
		}
	}
	if len(r.Capabilities) > 0 {
		names := make([]string, 0, len(r.Capabilities))
		for _, capability := range r.Capabilities {
			names = append(names, string(capability))
		}
		sort.Strings(names)
		fmt.Fprintf(b, "  %s only checked: %s\n", c.yellow("·"), strings.Join(names, ", "))
	}
	if len(r.Errors) > 0 {
		fmt.Fprintf(b, "\n  %s %d analyzer(s) failed to run: this report is incomplete:\n",
			c.red("✗"), len(r.Errors))
		for _, e := range r.Errors {
			fmt.Fprintf(b, "      %s\n", e)
		}
	}
}

func writeTimings(b *strings.Builder, r *Report, c colorizer) {
	if len(r.Timings) == 0 {
		return
	}
	fmt.Fprintf(b, "\n  %s\n", c.dim("Analyzer timings"))
	for _, t := range r.Timings {
		fmt.Fprintf(b, "    %-28s %8.1fms  %d finding%s\n",
			t.Name, t.Millis, t.Diagnostics, plural(t.Diagnostics))
	}
	fmt.Fprintf(b, "    %-28s %8.1fms\n", c.dim("total"), float64(r.Duration.Microseconds())/1000)
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// wrap hard-wraps prose at width, indenting continuation lines. Findings
// carry multi-sentence explanations; letting a terminal reflow them turns
// the report into a wall.
func wrap(s string, width int, indent string) string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return ""
	}
	var b strings.Builder
	lineLen := 0
	for i, w := range words {
		if i > 0 {
			if lineLen+1+len(w) > width {
				b.WriteString("\n" + indent)
				lineLen = 0
			} else {
				b.WriteString(" ")
				lineLen++
			}
		}
		b.WriteString(w)
		lineLen += len(w)
	}
	return b.String()
}

// colorizer keeps the ANSI decision in one place so every helper below
// degrades to plain text when stdout is redirected.
type colorizer struct{ on bool }

func (c colorizer) wrap(code, s string) string {
	if !c.on {
		return s
	}
	return "\033[" + code + "m" + s + "\033[0m"
}

func (c colorizer) red(s string) string    { return c.wrap("31", s) }
func (c colorizer) green(s string) string  { return c.wrap("32", s) }
func (c colorizer) yellow(s string) string { return c.wrap("33", s) }
func (c colorizer) cyan(s string) string   { return c.wrap("36", s) }
func (c colorizer) bold(s string) string   { return c.wrap("1", s) }
func (c colorizer) dim(s string) string    { return c.wrap("2", s) }

func (c colorizer) severity(s Severity) string {
	switch s {
	case SeverityError:
		return c.red("✗ error")
	case SeverityWarn:
		return c.yellow("⚠ warn ")
	default:
		return c.dim("· info ")
	}
}

// FormatExplain renders one catalog entry in full: the `--explain` view
// and the body of the `contracts_explain` MCP tool.
func FormatExplain(r Rule, color bool) string {
	c := colorizer{on: color}
	var b strings.Builder
	fmt.Fprintf(&b, "\n%s  %s\n", c.bold(r.ID), r.Title)
	fmt.Fprintf(&b, "%s\n\n", c.dim(fmt.Sprintf("%s · %s · default severity: %s", r.Capability, r.Slug, r.Severity)))
	fmt.Fprintf(&b, "%s\n  %s\n\n", c.bold("What"), wrap(r.Summary, 76, "  "))
	fmt.Fprintf(&b, "%s\n  %s\n\n", c.bold("Why"), wrap(r.Why, 76, "  "))
	fmt.Fprintf(&b, "%s\n  %s\n", c.bold("Fix"), wrap(r.Fix, 76, "  "))
	for _, ex := range r.Examples {
		b.WriteString("\n")
		if ex.Caption != "" {
			fmt.Fprintf(&b, "%s: %s\n", c.bold("Example"), ex.Caption)
		} else {
			fmt.Fprintf(&b, "%s\n", c.bold("Example"))
		}
		for _, line := range strings.Split(ex.Bad, "\n") {
			fmt.Fprintf(&b, "  %s %s\n", c.red("✗"), line)
		}
		for _, line := range strings.Split(ex.Good, "\n") {
			fmt.Fprintf(&b, "  %s %s\n", c.green("✓"), line)
		}
	}
	fmt.Fprintf(&b, "\n%s\n  %s\n  %s\n", c.bold("Docs"), r.DocCommand(), c.dim(r.DocURL()))
	fmt.Fprintf(&b, "\n%s\n  //gofastr:allow(%s) <reason>\n", c.bold("Suppress once"), r.ID)
	fmt.Fprintf(&b, "\n%s\n  contracts:\n    rules:\n      %s: warn   # or off\n", c.bold("Relax project-wide"), r.ID)
	return b.String()
}

// sanitizeText strips the raw control bytes repo content can carry (a
// rule reference, a source snippet, a file name) out of report text. An
// ESC reaching the terminal is escape-injection into the operator running
// `gofastr verify` on a hostile PR; NUL and VT corrupt framing the same
// way. Newline and tab stay: they are ordinary formatting. Applied at the
// FormatText print boundary so repo-derived strings cannot reach the
// terminal raw.
func sanitizeText(s string) string {
	return strings.Map(func(r rune) rune {
		if (r < 0x20 && r != '\n' && r != '\t') || r == 0x7f {
			return -1
		}
		return r
	}, s)
}
