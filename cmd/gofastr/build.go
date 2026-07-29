package main

import (
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/DonaldMurillo/gofastr/cmd/check-embed/embedcheck"
	"github.com/DonaldMurillo/gofastr/codegen"
)

func runBuild(args []string) {
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
