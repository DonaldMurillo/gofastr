package buildcmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/cmd/check-embed/embedcheck"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appgen"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/project"
	"github.com/DonaldMurillo/gofastr/codegen"
	"github.com/DonaldMurillo/gofastr/framework/contracts"
	"github.com/DonaldMurillo/gofastr/internal/contractsrun"
)

// RunBuild runs `gofastr build` and exits non-zero when a step fails.
func RunBuild(args []string) {
	if cli.HasHelpFlag(args) {
		printBuildUsage()
		return
	}
	fmt.Printf("\n  %s Building project...\n\n", cli.Bold("GoFastr"))

	start := time.Now()

	opts, err := parseBuildOptions(args)
	if err != nil {
		cli.Fail("%v", err)
		cli.OsExit(2)
	}

	if !opts.noGenerate {
		plan, err := appgen.PlanGenerate(appgen.GenerateOptions{})
		if err != nil {
			if errors.Is(err, codegen.ErrCommandExtensionNotAllowed) {
				cli.Fail("%s", buildCommandExtRefusal(plan.Discovery))
			} else {
				cli.Fail("Failed to load codegen config: %v", err)
			}
			cli.OsExit(1)
			return
		}
		if plan.HasWork() {
			cli.Info("Generating code...")
			plan.Run()
		}
	}

	if err := project.ValidateBuildTarget(context.Background(), opts.pkg); err != nil {
		cli.Fail("Build target %q is invalid: %v", opts.pkg, err)
		cli.OsExit(1)
	}

	cli.Info("Running go vet...")
	vetCmd := exec.Command("go", "vet", "./...")
	vetCmd.Stdout = os.Stdout
	vetCmd.Stderr = os.Stderr
	if err := vetCmd.Run(); err != nil {
		cli.Fail("go vet found issues")
		cli.OsExit(1)
	}
	cli.Success("go vet passed")

	if !opts.noA11y {
		cli.Info("Checking accessibility...")
		if !buildA11yGate(".") {
			cli.Fail("Accessibility lint failed. Fix the findings above (guided), or skip once with --no-a11y")
			cli.OsExit(1)
		}
		cli.Success("accessibility lint passed")
	}

	cli.Info("Checking .ui.go sandbox...")
	if !buildSandboxGate(".") {
		cli.Fail(".ui.go sandbox lint failed. Move goroutines/channels/IO out of .ui.go (see findings above)")
		cli.OsExit(1)
	}
	cli.Success(".ui.go sandbox lint passed")

	if !opts.noEmbedCheck {
		cli.Info("Checking embed surfaces for server actions...")
		if !buildEmbedGate("./...", opts.allowUnverifiedEmbeds) {
			cli.Fail("check-embed failed: an embeddable surface registers a G.serverAction, which is refused inside a frame. Fix the findings above, or skip once with --no-embed-check")
			cli.OsExit(1)
		}
		cli.Success("no embeddable surface registers a server action")
	}

	if !opts.noContracts {
		cli.Info("Verifying contracts...")
		if !buildContractsGate(".") {
			cli.Fail("Contract verification failed. Run `gofastr verify` for the full report, or skip once with --no-contracts")
			cli.OsExit(1)
		}
		cli.Success("contracts verified")
	}

	cli.Info("Compiling %s...", opts.pkg)
	buildCmd := exec.Command("go", "build", "-o", opts.output, opts.pkg)
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		cli.Fail("Build target %q failed", opts.pkg)
		cli.OsExit(1)
	}

	elapsed := time.Since(start)
	cli.Success("Build completed in %s", elapsed.Round(time.Millisecond))
	cli.Printf("  Binary: "+cli.Bold("%s")+"\n", opts.output)
}

func buildContractsGate(root string) bool {
	report, err := contractsrun.RunPipeline(root, "", contracts.RunOptions{}, true)
	if err != nil {
		cli.Fprintf(os.Stderr, "contracts: %v\n", err)
		return false
	}
	if report.Passed() && report.Counts.Warnings == 0 {
		return true
	}
	fmt.Print(contracts.FormatText(report, contracts.TextOptions{Color: cli.StdoutIsTTY}))
	return report.Passed()
}

func buildEmbedGate(pattern string, allowUnverified bool) bool {
	findings, unresolved, fset, err := embedcheck.CheckAll(pattern)
	if err != nil {
		cli.Fprintf(os.Stderr, "check-embed: %v\n", err)
		return false
	}
	var blocking int
	for _, u := range unresolved {
		cli.Fprintf(os.Stderr, "check-embed: %s: %s\n", fset.Position(u.Pos), u.Format())
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
			cli.Fprintf(os.Stderr, "%s: %s\n\n", fset.Position(f.Pos), f.Format())
		}
	}
	if blocking > 0 && !allowUnverified {
		cli.Fprintf(os.Stderr, "check-embed: %d embed surface path(s) could not be verified; see the notes above.\n"+
			"Hold the child in a field rather than building it in Render, or move its type into the surface's package. "+
			"If the surface genuinely cannot be analysed and you have verified it by hand, pass --allow-unverified-embeds.\n\n",
			blocking)
	}
	return false
}

func buildCommandExtRefusal(d codegen.Discovery) string {
	return fmt.Sprintf("%s: %s\n"+
		"Set GOFASTR_CODEGEN_ALLOW_COMMANDS=1 to allow commands from a discovered config, "+
		"or run gofastr generate --config %s and then gofastr build --no-generate",
		codegen.ErrCommandExtensionNotAllowed, d.CommandExtensionDetails(), shellWord(d.Path))
}

func shellWord(s string) string {
	plain := s != ""
	for i := 0; i < len(s) && plain; i++ {
		c := s[i]
		plain = c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte("_-./:@%+=,", c) >= 0
	}
	if plain {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
