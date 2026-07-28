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
		if !buildEmbedGate("./...") {
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
func buildEmbedGate(pattern string) bool {
	findings, fset, err := embedcheck.Check(pattern)
	if err != nil {
		fmt.Fprintf(os.Stderr, "check-embed: %v\n", err)
		return false
	}
	if len(findings) == 0 {
		return true
	}
	fmt.Fprintf(os.Stderr, "check-embed: %d embed surface(s) register a server action:\n\n", len(findings))
	for _, f := range findings {
		fmt.Fprintf(os.Stderr, "%s: %s\n\n", fset.Position(f.Pos), f.Format())
	}
	return false
}
