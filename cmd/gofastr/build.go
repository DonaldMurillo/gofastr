package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/DonaldMurillo/gofastr/cmd/check-embed/embedcheck"
	"github.com/DonaldMurillo/gofastr/codegen"
	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

func runBuild(args []string) {
	if hasHelpFlag(args) {
		printBuildUsage()
		return
	}
	fmt.Printf("\n  %s Building project...\n\n", bold("GoFastr"))

	start := time.Now()

	opts, err := parseBuildOptions(args)
	if err != nil {
		fail("%v", err)
		osExit(1)
	}

	if err := validateBuildTarget(opts.pkg); err != nil {
		fail("Build target %q is invalid: %v", opts.pkg, err)
		osExit(1)
	}

	// Step 1: run the codegen extension protocol when a gofastr.codegen.yml
	// is present. Blueprint generation (gofastr generate --from) is an
	// explicit, separate step — `gofastr build` does not guess a blueprint.
	if !opts.noGenerate {
		discovery, err := codegen.DiscoverConfig(".")
		if err != nil {
			fail("Failed to load codegen config: %v", err)
			osExit(1)
		}
		// Same gate as `gofastr generate`: a discovered config's command
		// extension runs a binary this build never asked for.
		if err := codegen.CheckCommandExtensions(discovery); err != nil {
			fail("%v", err)
			osExit(1)
		}
		if discovery.Found {
			info("Generating code...")
			generateProject(nil)
		}
	}

	// Step 2: go vet
	info("Running go vet...")
	vetCmd := exec.Command("go", "vet", "./...")
	vetCmd.Stdout = os.Stdout
	vetCmd.Stderr = os.Stderr
	if err := vetCmd.Run(); err != nil {
		fail("go vet found issues")
		osExit(1)
	}
	success("go vet passed")

	// Step 3: static accessibility lint. Enforced by default — the rules
	// are the WCAG floor the type system can already see (Alt on images,
	// Label on buttons/landmarks, …) and every finding ships with a fix
	// hint, so failing here is cheaper than failing a real user.
	// --no-a11y skips the gate for genuinely blocked builds.
	if !opts.noA11y {
		info("Checking accessibility...")
		if !buildA11yGate(".") {
			fail("Accessibility lint failed — fix the findings above (guided), or skip once with --no-a11y")
			osExit(1)
		}
		success("accessibility lint passed")
	}

	// Step 3b: .ui.go hydration-sandbox lint. The sandbox rules forbid
	// goroutines, channels, type switches, and imports outside the safe
	// allow-list in client-hydrated .ui.go files — they break hydration at
	// runtime. This is a correctness floor (not a WCAG nicety), so unlike
	// a11y it is NOT skippable: no valid .ui.go violates it.
	info("Checking .ui.go sandbox...")
	if !buildSandboxGate(".") {
		fail(".ui.go sandbox lint failed — move goroutines/channels/IO out of .ui.go (see findings above)")
		osExit(1)
	}
	success(".ui.go sandbox lint passed")

	// Step 4: embed surface server-action gate. G.serverAction is refused inside
	// a frame (the action registry is app-global, so honouring an embed grant
	// would let one surface's credential invoke any action), and framework/uihost
	// panics at boot when a surface's screen registers one. This catches it at
	// build time, with the same signal the boot walk uses. --no-embed-check
	// skips it for genuinely blocked builds.

	if !opts.noEmbedCheck {
		info("Checking embed surfaces for server actions...")
		if !buildEmbedGate("./...", opts.allowUnverifiedEmbeds) {
			fail("check-embed failed — an embeddable surface registers a G.serverAction, which is refused inside a frame. Fix the findings above, or skip once with --no-embed-check")
			osExit(1)
		}
		success("no embeddable surface registers a server action")
	}

	// Step 4b: the contract analyzers. Only findings at the fail-on
	// severity (error by default) stop the build — warnings print and let
	// it through, because a build that fails on "this route has no test"
	// is a build people learn to bypass. `gofastr verify` is where the
	// full picture lives; this is the floor.
	if !opts.noContracts {
		info("Verifying contracts...")
		if !buildContractsGate(".") {
			fail("Contract verification failed — run `gofastr verify` for the full report, or skip once with --no-contracts")
			osExit(1)
		}
		success("contracts verified")
	}

	// Step 5: go build
	info("Compiling %s...", opts.pkg)
	buildCmd := exec.Command("go", "build", "-o", opts.output, opts.pkg)
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		fail("Build target %q failed", opts.pkg)
		osExit(1)
	}

	elapsed := time.Since(start)
	success("Build completed in %s", elapsed.Round(time.Millisecond))
	fmt.Printf("  Binary: %s\n", bold(opts.output))
}

// buildContractsGate runs the contract analyzers for `gofastr build` and
// reports whether the build may proceed. It prints the same report
// `gofastr verify` does, so a developer who has only ever run `build`
// still learns the rule and the fix rather than a bare rejection.
func buildContractsGate(root string) bool {
	cfg, err := contracts.LoadConfig(root, "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "contracts: %v\n", err)
		return false
	}
	pass, err := contracts.NewPass(root, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "contracts: %v\n", err)
		return false
	}
	report, err := contracts.Run(pass, contracts.RunOptions{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "contracts: %v\n", err)
		return false
	}
	// `gofastr verify --baseline-write` is how an existing app adopts
	// contracts: accept what is there, fail on what is added. The build
	// gate has to honour the same file, or recording a baseline fixes
	// `verify` and leaves `build` permanently red — and the only exit a
	// user finds from that is --no-contracts, which turns everything off.
	baseline, baselineErr := contracts.ReadBaseline(filepath.Join(root, contracts.BaselineFileName))
	if baselineErr != nil {
		fmt.Fprintf(os.Stderr, "contracts: %v\n", baselineErr)
		return false
	}
	if baseline != nil {
		report.ApplyBaseline(baseline)
	}
	if report.Passed() && report.Counts.Warnings == 0 {
		return true
	}
	fmt.Print(contracts.FormatText(report, contracts.TextOptions{Color: stdoutIsTTY}))
	return report.Passed()
}

// buildEmbedGate runs the embed-surface server-action check for `gofastr build`
// and reports whether the build may proceed. It scans pattern (the package
// graph being built) via the shared embedcheck driver — the same one the
// standalone cmd/check-embed CLI uses — so build-time and CLI findings are
// identical. On a violation it prints each finding with its fix hint.
func buildEmbedGate(pattern string, allowUnverified bool) bool {
	findings, unresolved, fset, err := embedcheck.CheckAll(pattern)
	if err != nil {
		fmt.Fprintf(os.Stderr, "check-embed: %v\n", err)
		return false
	}
	// Print every note; fail only on the BLOCKING class.
	//
	// The boot-time walk in framework/uihost reads live component values, so a
	// child held in a field — through an interface, a map key, or an island
	// wrapper — is checked at Mount, and a note describing one is advisory.
	// The exception is a child CONSTRUCTED inside Render() whose type is in
	// another package: it does not exist as a value when the walk runs, and
	// its Actions() body is not in the analyzer's syntax tree. Neither gate
	// can vouch for it, so that one stops the build.
	//
	// Failing on every note was tried and reverted: it also rejected clean
	// island surfaces (the shape the blueprint emits for every island block),
	// interface-typed fields the analyzer had already resolved, and the
	// fixture named for false positives — with no remedy available, since
	// "hold the child in a field" is impossible for a wrapper.
	var blocking int
	for _, u := range unresolved {
		fmt.Fprintf(os.Stderr, "check-embed: %s: %s\n", fset.Position(u.Pos), u.Format())
		if u.Blocking {
			blocking++
		}
	}
	if len(findings) == 0 && (blocking == 0 || allowUnverified) {
		return true
	}
	if len(findings) > 0 {
		fmt.Fprintf(os.Stderr, "check-embed: %d embed surface(s) register a server action:\n\n", len(findings))
		for _, f := range findings {
			fmt.Fprintf(os.Stderr, "%s: %s\n\n", fset.Position(f.Pos), f.Format())
		}
	}
	if blocking > 0 && !allowUnverified {
		fmt.Fprintf(os.Stderr, "check-embed: %d embed surface path(s) could not be verified — see the notes above.\n"+
			"Hold the child in a field rather than building it in Render, or move its type into the surface's package. "+
			"If the surface genuinely cannot be analysed and you have verified it by hand, pass --allow-unverified-embeds.\n\n",
			blocking)
	}
	return false
}
