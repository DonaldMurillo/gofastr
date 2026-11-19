package buildcmd

import (
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
)

type buildOptions struct {
	output       string
	pkg          string
	noGenerate   bool
	noA11y       bool
	noEmbedCheck bool
	// noContracts skips the contract analyzers. The gate only fails on
	// error-severity findings, so this exists for a build that must ship
	// while a rule is being addressed, not as a routine flag.
	noContracts bool
	// allowUnverifiedEmbeds downgrades check-embed's unresolved notes from
	// fatal back to advisory, without disabling the check the way
	// --no-embed-check does. It exists for the surface that genuinely cannot
	// be analysed statically and has been verified by hand; provable
	// violations still fail the build.
	allowUnverifiedEmbeds bool
}

func parseBuildOptions(args []string) (buildOptions, error) {
	opts := buildOptions{output: "bin/server", pkg: "."}
	_, err := cli.ParseFlags(args, map[string]*string{
		"--pkg":    &opts.pkg,
		"--output": &opts.output,
		"-o":       &opts.output,
	}, map[string]*bool{
		"--no-generate":             &opts.noGenerate,
		"--no-a11y":                 &opts.noA11y,
		"--no-embed-check":          &opts.noEmbedCheck,
		"--no-contracts":            &opts.noContracts,
		"--allow-unverified-embeds": &opts.allowUnverifiedEmbeds,
	}, validateBuildValue)
	if err != nil {
		return buildOptions{}, fmt.Errorf("build: %w", err)
	}
	return opts, nil
}

func validateBuildValue(flag, value string) error {
	switch flag {
	case "--pkg":
		if value == "" || strings.HasPrefix(value, "-") {
			return fmt.Errorf("%s requires a package path", flag)
		}
	case "--output", "-o":
		if value == "" {
			return fmt.Errorf("%s requires an output path", flag)
		}
	}
	return nil
}
