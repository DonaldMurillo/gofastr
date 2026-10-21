package main

import (
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/codegen"
)

// generatorKind says what a generator's output is for, and so who may run
// it: a person, once, or every build.
type generatorKind int

const (
	// scaffoldKind writes code into the repo once; the user owns it from
	// then on. A second run refuses to overwrite it unless told to (--force),
	// so a build can never re-run one.
	scaffoldKind generatorKind = iota
	// derivedKind keeps a type-safe layer in sync with a source (the
	// entities, the style sheets). Its files say DO NOT EDIT, and every
	// plain `gofastr generate`, which `gofastr build` calls, rebuilds them
	// from the codegen config.
	derivedKind
)

func (k generatorKind) String() string {
	if k == derivedKind {
		return "derived"
	}
	return "scaffold"
}

// generatorSpec is one entry in the generator registry: the name
// `gofastr generate <name>` dispatches on, its kind, and its help line.
type generatorSpec struct {
	name    string
	kind    generatorKind
	usage   string
	summary string
	run     func(args []string)
}

// generators is the one registry every generator lives in. The help text,
// the subcommand dispatch, the codegen-config check and the provenance
// marker all read it, so a new generator is one entry here. init fills it:
// the run funcs stamp their output through this registry, and a package
// var initializer may not refer back to itself.
var generators []generatorSpec

func init() {
	generators = []generatorSpec{
		{name: "app", kind: scaffoldKind, usage: "app [spec] [--out=<dir>] [--force | --add] [--dry-run] [--json]",
			summary: "Generate an app from an app spec (default gofastr.yml; a file or a directory)", run: runGenerateApp},
		{name: "entity", kind: scaffoldKind, usage: "entity <name> [--out=<dir>] [--dry-run] [--json]",
			summary: "Add an editable entity stub to a generated app", run: generateScaffoldEntity},
		{name: "screen", kind: scaffoldKind, usage: "screen <name> [--from-a11y=<file>] [--out=<dir>] [--dry-run] [--json]",
			summary: "Add an editable screen stub, or build it from a Playwright aria snapshot", run: generateScaffoldScreen},
		{name: "package", kind: scaffoldKind, usage: "package [<name>] [--out=<dir>] [--dry-run]",
			summary: "Copy a canonical chrome package (siteheader, sitefooter, docpage) into the app; no name lists them", run: runGeneratePackage},
		{name: "client", kind: derivedKind, usage: "client [--force] [--dry-run] [--json]",
			summary: "Rebuild entities/client, the app's typed Go client, from entities/", run: runGenerateClient},
		{name: "cli", kind: derivedKind, usage: "cli [flags]",
			summary: "Generate a customer-facing CLI for the app API (custom.go stays yours)", run: runGenerateCLI},
		{name: "sdk", kind: derivedKind, usage: "sdk [flags]",
			summary: "Generate Go and JavaScript/TypeScript API clients", run: runGenerateSDK},
		{name: "styles", kind: derivedKind, usage: "styles [patterns]",
			summary: "Typed Go for app tokens and owned style sheets", run: runGenerateStyles},
	}
}

func lookupGenerator(name string) (generatorSpec, bool) {
	for _, g := range generators {
		if g.name == name {
			return g, true
		}
	}
	return generatorSpec{}, false
}

func generatorNames() string {
	names := make([]string, 0, len(generators))
	for _, g := range generators {
		names = append(names, g.name)
	}
	return strings.Join(names, ", ")
}

// checkConfigGenerators refuses a codegen config that lists a scaffold
// generator. The config is what plain `gofastr generate` and every build
// re-run, and re-running a scaffold generator would either refuse (the
// files exist) or, forced, overwrite code the user owns.
func checkConfigGenerators(cfg codegen.CodegenConfig) error {
	for i, gen := range cfg.Generators {
		if gen.Extension != "" {
			continue
		}
		g, ok := lookupGenerator(gen.Name)
		if !ok || g.kind != scaffoldKind {
			continue
		}
		return fmt.Errorf("codegen.generators[%d]: %q is a scaffold generator: it writes code you own, once, and every build re-runs the codegen config. Run `gofastr generate %s` yourself instead; the config lists derived generators (%s) and extensions", i, gen.Name, gen.Name, derivedGeneratorNames())
	}
	return nil
}

func derivedGeneratorNames() string {
	var names []string
	for _, g := range generators {
		if g.kind == derivedKind {
			names = append(names, g.name)
		}
	}
	return strings.Join(names, ", ")
}

// generatorHelp renders the registry for `gofastr generate --help`,
// grouped by kind.
func generatorHelp() string {
	var b strings.Builder
	for _, kind := range []generatorKind{scaffoldKind, derivedKind} {
		if kind == scaffoldKind {
			b.WriteString("Scaffold generators write code you own, once (a second run needs --force):\n")
		} else {
			b.WriteString("\nDerived generators rebuild DO NOT EDIT code from a source on every run:\n")
		}
		for _, g := range generators {
			if g.kind != kind {
				continue
			}
			fmt.Fprintf(&b, "  gofastr generate %s\n      %s\n", g.usage, g.summary)
		}
	}
	return b.String()
}
