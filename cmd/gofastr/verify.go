package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
	_ "github.com/DonaldMurillo/gofastr/framework/contracts/analyzers"
)

// runVerify is `gofastr verify` — one pipeline that answers "is this a
// good GoFastr application", where `go build` and `go vet` only answer
// "does it compile" and "is anything obviously wrong".
//
//	gofastr verify                     everything
//	gofastr verify routing security    only those capabilities
//	gofastr verify --json              machine-readable, for agents and CI
//	gofastr verify --sarif out.sarif   for code scanning and IDEs
//	gofastr verify --fix               apply the mechanical fixes
//	gofastr verify --explain GOFASTR1002
//	gofastr verify --list              the whole rule catalog
func runVerify(args []string) {
	opts, err := parseVerifyArgs(args)
	if err != nil {
		fail("%v", err)
		osExit(2)
		return
	}
	if opts.help {
		printVerifyUsage()
		return
	}
	if opts.explain != "" {
		explainRule(opts.explain, opts.json)
		return
	}
	if opts.list {
		listRules(opts.json, opts.capabilities)
		return
	}

	// A baseline is "the debt this whole tree accepts". Recording it from
	// a narrowed run would silently erase every other rule's entries, and
	// the next full verify/build fails on findings the team had already
	// signed off — so the combination is refused, not reinterpreted.
	if opts.baselineWrite && (len(opts.rules) > 0 || len(opts.capabilities) > 0 || len(opts.analyzers) > 0) {
		failRun(opts.json, "--baseline-write records the whole tree's accepted debt; combined with --rule or a capability filter it would erase every other rule's entries — run it without filters")
		osExit(2)
		return
	}

	cfg, err := contracts.LoadConfig(opts.root, opts.configPath)
	if err != nil {
		failRun(opts.json, "%v", err)
		osExit(2)
		return
	}
	if opts.strict {
		cfg.FailOn = contracts.SeverityWarn
	}

	pass, err := contracts.NewPass(opts.root, cfg)
	if err != nil {
		failRun(opts.json, "%v", err)
		osExit(2)
		return
	}

	// `go vet` runs first and its failure is fatal. A contract report on
	// code the compiler already rejects is noise at best and misleading at
	// worst — half the analyzers cannot parse what they need.
	//
	// This runs in --json mode too. It used to be skipped there, which
	// meant the consumer most likely to act on the output — an agent —
	// was the only one that got a report on code that does not build,
	// with nothing in the payload admitting it.
	vet := &contracts.VetResult{}
	switch {
	case opts.noVet:
		vet.Skipped = "--no-vet"
	default:
		if !opts.json {
			info("Running go vet...")
		}
		vet.Ran = true
		out, ok := runGoVet(opts.root, opts.json)
		vet.Passed = ok
		if !ok {
			// Mirror the text path: report the precondition failure and
			// no diagnostics, rather than a wall of findings the broken
			// build makes unreliable.
			vet.Output = out
			if opts.json {
				emitVetFailureJSON(opts.root, cfg, vet)
			} else {
				fail("go vet found issues — fix those first")
			}
			osExit(1)
			return
		}
		if !opts.json {
			success("go vet passed")
		}
	}

	report, err := contracts.Run(pass, contracts.RunOptions{
		Capabilities: opts.capabilities,
		Analyzers:    opts.analyzers,
	})
	if err != nil {
		failRun(opts.json, "%v", err)
		osExit(2)
		return
	}
	// Report.Only treats an unrecognised name as "match nothing", which
	// is the safe default for a caller applying fixes. At the CLI it
	// would turn a typo into a clean run, so reject it here. The
	// narrowing itself happens LAST, with --changed's: narrowing here
	// handed the baseline an Only() copy stripped of the suppression
	// identities it needs, so `--rule` reopened the suppressed-slot hole
	// and misreported other rules' baseline entries as over-accepting.
	for _, name := range opts.rules {
		if _, ok := contracts.LookupRule(name); !ok {
			failRun(opts.json, "unknown rule %q — run `gofastr verify --list` to see the catalog", name)
			osExit(2)
			return
		}
	}

	// success/info/warn all write to STDOUT, so anything they print in
	// --json mode lands in front of the document and breaks every
	// consumer. The vet stage was fixed for exactly this; these paths were
	// not. Collect instead, print in text mode, and marshal in JSON mode
	// so the information is moved rather than lost.
	var notices []string
	note := func(format string, args ...any) {
		msg := fmt.Sprintf(format, args...)
		notices = append(notices, msg)
		if !opts.json {
			info("%s", msg)
		}
	}
	noteWarn := func(format string, args ...any) {
		msg := fmt.Sprintf(format, args...)
		notices = append(notices, msg)
		if !opts.json {
			warn("%s", msg)
		}
	}

	// The changed-file set is resolved before the fix stage, not just
	// before reporting. --changed --fix in a pre-commit hook used to
	// rewrite the whole tree and then report only the changed files, so
	// an unrelated file was edited without appearing in the output the
	// user was reading.
	var changedFiles map[string]bool
	if opts.changed {
		files, changedErr := contracts.ChangedFiles(opts.root, opts.changedRef)
		if changedErr != nil {
			failRun(opts.json, "%v", changedErr)
			osExit(2)
			return
		}
		if files == nil {
			noteWarn("--changed: %s is not a git repository — reporting everything", opts.root)
		} else {
			changedFiles = files
		}
	}

	var fixed []contracts.FixedDiagnostic
	if opts.fix {
		// Say up front when nothing CAN be fixed. Without this the user
		// asks to fix a rule, gets the finding back unchanged, and cannot
		// tell whether the tool failed or the rule simply has no
		// mechanical fix — which is what `contracts_fix` over MCP has
		// always said outright.
		noteUnfixableRules(opts.rules, noteWarn, note)

		// The fix pass is scoped by --rule and --changed; the REPORT
		// stays whole until the baseline has been applied to it.
		fixScope := report
		if len(opts.rules) > 0 {
			fixScope = fixScope.Only(opts.rules...)
		}
		applied, applyErr := fixScope.OnlyFiles(changedFiles).Apply()
		if applyErr != nil {
			// Apply writes file by file and stops at the first refusal —
			// the files before it are already rewritten, and an error
			// alone would report a clean failure over a half-changed
			// tree. In the message rather than a notice, so the JSON
			// mode's exit-2 path carries it too.
			if len(applied) > 0 {
				failRun(opts.json, "%v — %s", applyErr, partialWriteNote(applied))
			} else {
				failRun(opts.json, "%v", applyErr)
			}
			osExit(2)
			return
		}
		if len(applied) > 0 {
			// Re-run rather than subtracting the fixed findings from the
			// old report: an edit can resolve a finding the fixer never
			// looked at, and printing a stale list right after changing
			// files on disk is how a tool loses trust.
			for _, d := range applied {
				fixed = append(fixed, contracts.FixedDiagnostic{Rule: d.RuleID, File: d.File, Line: d.Line})
				if !opts.json {
					success("fixed %s at %s", d.RuleID, d.Location())
				}
			}
			pass, err = contracts.NewPass(opts.root, cfg)
			if err == nil {
				report, err = contracts.Run(pass, contracts.RunOptions{
					Capabilities: opts.capabilities,
					Analyzers:    opts.analyzers,
				})
			}
			if err != nil {
				failRun(opts.json, "re-verify after fixes: %v", err)
				osExit(2)
				return
			}
		} else if len(fixScope.Diagnostics) > 0 {
			// Distinguish "nothing is fixable" from "nothing in scope is
			// fixable". With --changed the fix is narrowed to the changed
			// files, so fixable findings can exist and still not be
			// touched — saying none carry a fix would be false.
			switch {
			case changedFiles != nil && len(fixScope.Fixable()) > 0:
				n := len(fixScope.Fixable())
				note("nothing to fix in the changed files — %d fixable finding%s elsewhere; run without --changed to apply them.",
					n, map[bool]string{true: " sits", false: "s sit"}[n == 1])
			default:
				// "In scope", not "in this report": the fix pass runs
				// before the baseline is applied (paying down accepted
				// debt is allowed), so findings it looked at may not be
				// visible in the report printed below.
				note("nothing to fix — nothing in scope carries a mechanical fix, baselined debt included; `gofastr verify --explain <rule>` writes out the fix to apply by hand.")
			}
		}
	}

	// The baseline is applied AFTER any --fix pass, so a fix that resolves
	// baselined debt shows up as an over-accepting entry rather than
	// silently keeping its allowance.
	baselineFile := opts.baselinePath
	if baselineFile == "" {
		baselineFile = filepath.Join(opts.root, contracts.BaselineFileName)
	}
	if opts.baselineWrite {
		b := contracts.NewBaseline(report, time.Now(), "recorded by gofastr verify --baseline-write")
		if writeErr := contracts.WriteBaseline(baselineFile, b); writeErr != nil {
			failRun(opts.json, "%v", writeErr)
			osExit(2)
			return
		}
		// The success half of this path must keep stdout parseable too —
		// the failure half already ships through failRun.
		if opts.json {
			data, marshalErr := json.Marshal(map[string]any{
				"baselineWritten": baselineFile,
				"accepted":        b.Total(),
			})
			if marshalErr == nil {
				fmt.Println(string(data))
			}
		} else {
			success("baseline written to %s — %d finding(s) accepted", baselineFile, b.Total())
			info("New findings will now fail; these will not. Re-record as you pay the debt down.")
		}
		return
	}
	// An explicit --baseline must exist; the conventional file is optional,
	// so a project without one is not nagged.
	baseline, baselineErr := contracts.ReadBaseline(baselineFile)
	if baselineErr != nil {
		failRun(opts.json, "%v", baselineErr)
		osExit(2)
		return
	}
	if baseline == nil && opts.baselineSet {
		failRun(opts.json, "baseline %s does not exist — record one with `gofastr verify --baseline-write`", baselineFile)
		osExit(2)
		return
	}
	if baseline != nil {
		report.ApplyBaseline(baseline)
	}

	// Narrowing happens last, so a finding is only hidden by --changed or
	// --rule after the baseline has had its say — applied to a narrowed
	// copy, the baseline lost the suppression identities that keep a
	// suppressed finding's slot occupied, and counted every other rule's
	// entries as over-accepting. The analysis itself always ran
	// whole-tree: a duplicate route introduced by editing one file is
	// still found, because the other half of the pair was analysed too.
	if changedFiles != nil {
		report.RestrictTo(changedFiles)
	}
	if len(opts.rules) > 0 {
		report = report.Only(opts.rules...)
	}

	// The vet stage is part of the run's meaning, not a side effect of it:
	// a reader has to be able to tell a clean report from one whose
	// precondition was never checked.
	report.Vet = vet
	report.Notices = notices
	report.Fixed = fixed

	// SARIF is a FILE destination, not a stdout format — it is written
	// whenever asked for, whatever stdout carries. Inside the format
	// switch, `--json --sarif out.sarif` silently dropped the file.
	if opts.sarifPath != "" {
		data, marshalErr := contracts.FormatSARIF(report, version)
		if marshalErr != nil {
			failRun(opts.json, "%v", marshalErr)
			osExit(2)
			return
		}
		if writeErr := os.WriteFile(opts.sarifPath, append(data, '\n'), 0o644); writeErr != nil {
			failRun(opts.json, "write %s: %v", opts.sarifPath, writeErr)
			osExit(2)
			return
		}
		if !opts.json {
			success("SARIF written to %s (%d result(s))", opts.sarifPath, len(report.Diagnostics))
		}
	}
	switch {
	case opts.json:
		data, marshalErr := contracts.FormatJSON(report)
		if marshalErr != nil {
			failRun(opts.json, "%v", marshalErr)
			osExit(2)
			return
		}
		fmt.Println(string(data))
	case opts.sarifPath != "":
		// The success line above plus, on failure, the verdict — a
		// failing run must not end on a green line alone.
		if !report.Passed() {
			fail("%d error(s), %d warning(s) — details in %s",
				report.Counts.Errors, report.Counts.Warnings, opts.sarifPath)
		}
	default:
		fmt.Print(contracts.FormatText(report, contracts.TextOptions{
			Color:   stdoutIsTTY,
			Verbose: opts.verbose,
			Timings: opts.timings,
		}))
		printVerifyOutcome(report, opts.fix)
	}

	osExit(report.ExitCode())
}

// printVerifyOutcome is the closing line: what happened, and what to run
// next. A report that ends in a list of problems and nothing else leaves
// the reader to work out the next move.
func printVerifyOutcome(report *contracts.Report, fixed bool) {
	if report.Passed() {
		fmt.Println()
		success("Contracts verified.")
		return
	}
	fmt.Println()
	fail("%d error(s), %d warning(s) — see above.", report.Counts.Errors, report.Counts.Warnings)
	if n := len(report.Fixable()); n > 0 && !fixed {
		info("%d finding(s) can be fixed mechanically: gofastr verify --fix", n)
	}
	if len(report.Diagnostics) > 0 {
		info("Explain any rule: gofastr verify --explain %s", report.Diagnostics[0].RuleID)
	}
}

// runGoVet reports whether `go vet` passed, along with its output. In
// text mode the output streams straight through so it appears as vet
// printed it; in JSON mode it is captured instead, because anything on
// stdout would corrupt the document.
func runGoVet(root string, capture bool) (string, bool) {
	cmd := exec.Command("go", "vet", "./...")
	cmd.Dir = root
	if capture {
		var buf strings.Builder
		cmd.Stdout, cmd.Stderr = &buf, &buf
		err := cmd.Run()
		return strings.TrimSpace(buf.String()), err == nil
	}
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return "", cmd.Run() == nil
}

// emitVetFailureJSON writes the document for a run that stopped at the
// vet stage: passed=false, no diagnostics, and the vet output attached so
// the reader does not have to re-run anything to see what broke.
func emitVetFailureJSON(root string, cfg *contracts.Config, vet *contracts.VetResult) {
	report := &contracts.Report{Root: root, ConfigPath: cfg.Path, FailOn: cfg.FailOn, Vet: vet}
	data, err := contracts.FormatJSON(report)
	if err != nil {
		fail("render JSON: %v", err)
		return
	}
	fmt.Println(string(data))
}

func explainRule(idOrSlug string, asJSON bool) {
	rule, ok := contracts.LookupRule(idOrSlug)
	if !ok {
		fail("unknown rule %q", idOrSlug)
		if near := contracts.SuggestRules(idOrSlug); len(near) > 0 {
			info("did you mean: %s", strings.Join(near, ", "))
		}
		info("run `gofastr verify --list` for the catalog")
		osExit(2)
		return
	}
	if asJSON {
		data, err := contracts.FormatCatalogJSON([]contracts.Rule{rule})
		if err != nil {
			fail("%v", err)
			osExit(2)
			return
		}
		fmt.Println(string(data))
		return
	}
	fmt.Print(contracts.FormatExplain(rule, stdoutIsTTY))
}

func listRules(asJSON bool, capabilities []contracts.Capability) {
	rules := contracts.AllRules()
	if len(capabilities) > 0 {
		want := map[contracts.Capability]bool{}
		for _, c := range capabilities {
			want[c] = true
		}
		filtered := rules[:0]
		for _, r := range rules {
			if want[r.Capability] {
				filtered = append(filtered, r)
			}
		}
		rules = filtered
	}
	if asJSON {
		data, err := contracts.FormatCatalogJSON(rules)
		if err != nil {
			fail("%v", err)
			osExit(2)
			return
		}
		fmt.Println(string(data))
		return
	}
	fmt.Printf("\n%s — %d rules\n", bold("GoFastr contracts"), len(rules))
	current := contracts.Capability("")
	for _, r := range rules {
		if r.Capability != current {
			current = r.Capability
			fmt.Printf("\n%s\n", bold(current.Title()))
		}
		fmt.Printf("  %s  %-8s %-34s %s\n",
			r.ID, dim(r.Severity.String()), r.Slug, r.Title)
	}
	fmt.Printf("\n%s\n", dim("gofastr verify --explain <id>   full description, why it matters, and the fix"))
}

// ------------------------------------------------------------------
// Argument parsing
// ------------------------------------------------------------------

type verifyOptions struct {
	root          string
	configPath    string
	capabilities  []contracts.Capability
	analyzers     []string
	rules         []string
	explain       string
	sarifPath     string
	baselinePath  string
	baselineSet   bool
	baselineWrite bool
	changedRef    string
	changed       bool
	json          bool
	list          bool
	fix           bool
	strict        bool
	verbose       bool
	timings       bool
	noVet         bool
	help          bool
}

func parseVerifyArgs(args []string) (verifyOptions, error) {
	opts := verifyOptions{root: "."}
	sawRoot := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		// Accept both `--flag value` and `--flag=value`; the docs use the
		// space form and people type both.
		for _, valued := range []string{"--config", "--explain", "--sarif", "--analyzer", "--rule", "--root", "--baseline"} {
			if arg == valued && i+1 < len(args) {
				arg = valued + "=" + args[i+1]
				i++
				break
			}
		}
		switch {
		case arg == "--help" || arg == "-h":
			opts.help = true
		case arg == "--json":
			opts.json = true
		case arg == "--list":
			opts.list = true
		case arg == "--fix":
			opts.fix = true
		case arg == "--strict":
			opts.strict = true
		case arg == "--verbose" || arg == "-v":
			opts.verbose = true
		case arg == "--timings":
			opts.timings = true
		case arg == "--no-vet":
			opts.noVet = true
		case strings.HasPrefix(arg, "--config="):
			opts.configPath = strings.TrimPrefix(arg, "--config=")
		case strings.HasPrefix(arg, "--explain="):
			opts.explain = strings.TrimPrefix(arg, "--explain=")
		case strings.HasPrefix(arg, "--sarif="):
			opts.sarifPath = strings.TrimPrefix(arg, "--sarif=")
		case arg == "--baseline-write":
			opts.baselineWrite = true
		case arg == "--changed":
			opts.changed = true
		case strings.HasPrefix(arg, "--changed="):
			opts.changed = true
			opts.changedRef = strings.TrimPrefix(arg, "--changed=")
		case strings.HasPrefix(arg, "--baseline="):
			opts.baselinePath = strings.TrimPrefix(arg, "--baseline=")
			opts.baselineSet = true
		case strings.HasPrefix(arg, "--root="):
			opts.root = strings.TrimPrefix(arg, "--root=")
			sawRoot = true
		case strings.HasPrefix(arg, "--rule="):
			for _, name := range strings.Split(strings.TrimPrefix(arg, "--rule="), ",") {
				if name = strings.TrimSpace(name); name != "" {
					opts.rules = append(opts.rules, name)
				}
			}
		case strings.HasPrefix(arg, "--analyzer="):
			for _, name := range strings.Split(strings.TrimPrefix(arg, "--analyzer="), ",") {
				if name = strings.TrimSpace(name); name != "" {
					opts.analyzers = append(opts.analyzers, name)
				}
			}
		case strings.HasPrefix(arg, "-"):
			return opts, fmt.Errorf("unknown flag %s — run `gofastr verify --help`", arg)
		default:
			// A bare word is a capability filter. A path is only accepted
			// as the root when it is not also a capability name, which is
			// why `--root` exists for the ambiguous cases.
			capability, capErr := contracts.ParseCapability(arg)
			if capErr == nil {
				opts.capabilities = append(opts.capabilities, capability)
				continue
			}
			if sawRoot {
				return opts, capErr
			}
			if info, statErr := os.Stat(arg); statErr == nil && info.IsDir() {
				opts.root, sawRoot = arg, true
				continue
			}
			return opts, capErr
		}
	}
	return opts, nil
}

func printVerifyUsage() {
	fmt.Printf(`
%s — check the app against the GoFastr contract

%s:
  gofastr verify [capability...] [flags]

Runs go vet, then every contract analyzer, and reports what does not hold.
Strict by default: every rule in the catalog is enforced at its declared
severity unless a gofastr.contracts.yml relaxes it, or a
%s directive on the line waives one instance.

%s:
  meta  routing  permissions  security  data  entities
  architecture  rendering  accessibility  performance  testing  ai

%s:
  --list             Print the rule catalog and exit
  --explain <rule>   Print one rule in full — why it matters and how to fix it
  --json             Machine-readable report (every diagnostic carries its rule)
  --sarif <file>     Write SARIF 2.1.0 for code scanning / IDE integration
  --fix              Apply the mechanical fixes, then re-verify
  --baseline-write   Record every current finding as accepted debt, so only
                     NEW findings fail from now on. How an existing codebase
                     adopts verify without fixing everything first.
  --baseline <file>  Use a baseline other than .gofastr-contracts-baseline.json
  --changed[=<ref>]  Report only findings in files this change touched.
                     Bare form = uncommitted work; with a ref = everything
                     since the fork point with it (e.g. --changed=main).
                     The analysis still runs whole-tree, so cross-file
                     findings are still found — only reporting narrows.
  --strict           Warnings fail the run too
  --config <file>    Config file (default: gofastr.contracts.yml, or a
                     contracts: block in gofastr.yml)
  --analyzer <name>  Run only the named analyzer(s), comma-separated
  --rule <id>        Report only the named rule(s), comma-separated. IDs or
                     slugs. Pair with --fix to apply one rule's fixes at a
                     time, so the edits stay reviewable.
  --root <dir>       Project root (default: .)
  --no-vet           Skip the go vet stage
  --timings          Show per-analyzer wall time
  --verbose          Show the full explanation under every finding
  --help             This message

%s:
  gofastr verify
  gofastr verify security permissions
  gofastr verify --explain GOFASTR1002
  gofastr verify --json | jq '.diagnostics[] | select(.severity=="error")'
  gofastr verify --sarif verify.sarif

%s: 0 clean · 1 findings at or above the fail-on severity · 2 bad usage
`,
		bold("gofastr verify"), bold("Usage"), bold("//gofastr:allow(RULE)"),
		bold("Capabilities"), bold("Flags"), bold("Examples"), bold("Exit codes"))
}

// failRun reports an operational failure of the verify run. In text mode
// it is fail(); in --json mode stdout IS the document on every path, so
// the failure ships AS the document — `{"error": "..."}` — with the
// prose copied to stderr for a human watching a pipeline log. fail()
// writes to stdout, and prose in front of a document a consumer is
// parsing corrupts it on exactly the runs that went wrong.
func failRun(jsonMode bool, format string, args ...any) {
	if !jsonMode {
		fail(format, args...)
		return
	}
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(os.Stderr, "  ✗ %s\n", msg)
	data, err := json.Marshal(map[string]string{"error": msg})
	if err != nil {
		// A string map cannot fail to marshal; belt for the impossible.
		data = []byte(`{"error":"verify failed and the message could not be encoded"}`)
	}
	fmt.Println(string(data))
}

// partialWriteNote names the files an aborted fix pass already rewrote.
func partialWriteNote(applied []contracts.Diagnostic) string {
	files := map[string]bool{}
	for _, d := range applied {
		files[d.File] = true
	}
	n := len(applied)
	return fmt.Sprintf("%d fix%s already written to %s; the tree has changed, re-run verify",
		n, map[bool]string{true: " was", false: "es were"}[n == 1],
		strings.Join(contracts.SortedFiles(files), ", "))
}

// noteUnfixableRules warns for each --rule the user asked to fix that has
// no autofix. It is advisory rather than fatal: naming three rules where
// two are fixable should still fix those two.
func noteUnfixableRules(names []string, noteWarn, note func(string, ...any)) {
	for _, name := range names {
		rule, ok := contracts.LookupRule(name)
		if !ok || rule.Autofix {
			continue
		}
		noteWarn("%s (%s) has no autofix — it has to be applied by hand.", rule.ID, rule.Slug)
		note("why: gofastr verify --explain %s", rule.ID)
	}
}
