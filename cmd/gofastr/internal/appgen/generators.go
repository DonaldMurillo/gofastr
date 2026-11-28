package appgen

import (
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/codegen"
)

// GeneratorKind says who may run a generator: a person, once (scaffold), or every build (derived).
type GeneratorKind int

const (
	// ScaffoldKind writes code the user owns from then on; a second run needs --force.
	ScaffoldKind GeneratorKind = iota
	// DerivedKind rebuilds DO NOT EDIT files from a source on every plain gofastr generate.
	DerivedKind
)

func (k GeneratorKind) String() string {
	if k == DerivedKind {
		return "derived"
	}
	return "scaffold"
}

type generatorSpec struct {
	Name    string
	Kind    GeneratorKind
	Usage   string
	Summary string
	Run     func(args []string)
}

// Generators is the registry the help text, dispatch, codegen-config check and provenance markers read.
var Generators []generatorSpec

func init() {
	Generators = []generatorSpec{
		{Name: "app", Kind: ScaffoldKind, Usage: "app [spec] [--out=<dir>] [--force | --add] [--dry-run] [--json]",
			Summary: "Generate an app from an app spec (default gofastr.yml; a file or a directory)", Run: RunGenerateApp},
		{Name: "entity", Kind: ScaffoldKind, Usage: "entity <name> [--out=<dir>] [--dry-run] [--json]",
			Summary: "Add an editable entity stub to a generated app", Run: GenerateScaffoldEntity},
		{Name: "screen", Kind: ScaffoldKind, Usage: "screen <name> [--from-a11y=<file>] [--out=<dir>] [--dry-run] [--json]",
			Summary: "Add an editable screen stub, or build it from a Playwright aria snapshot", Run: GenerateScaffoldScreen},
		{Name: "package", Kind: ScaffoldKind, Usage: "package [<name>] [--out=<dir>] [--dry-run]",
			Summary: "Copy a canonical chrome package (siteheader, sitefooter, docpage) into the app; no name lists them", Run: runGeneratePackage},
		{Name: "endpoints", Kind: DerivedKind, Usage: "endpoints [--dry-run] [--json]",
			Summary: "Rebuild entities/endpoints.gen.go from the endpoints section of gofastr.settings.yml", Run: runGenerateEndpoints},
		{Name: "client", Kind: DerivedKind, Usage: "client [--force] [--dry-run] [--json]",
			Summary: "Rebuild entities/client, the app's typed Go client, from entities/", Run: RunGenerateClient},
		{Name: "cli", Kind: DerivedKind, Usage: "cli [flags]",
			Summary: "Generate a customer-facing CLI for the app API (custom.go stays yours)", Run: RunGenerateCLI},
		{Name: "sdk", Kind: DerivedKind, Usage: "sdk [flags]",
			Summary: "Generate Go and JavaScript/TypeScript API clients", Run: RunGenerateSDK},
		{Name: "styles", Kind: DerivedKind, Usage: "styles [patterns]",
			Summary: "Typed Go for app tokens and owned style sheets", Run: runGenerateStyles},
	}
}

func LookupGenerator(name string) (generatorSpec, bool) {
	for _, g := range Generators {
		if g.Name == name {
			return g, true
		}
	}
	return generatorSpec{}, false
}

func checkConfigGenerators(cfg codegen.CodegenConfig) error {
	for i, gen := range cfg.Generators {
		if gen.Extension != "" {
			continue
		}
		g, ok := LookupGenerator(gen.Name)
		if !ok || g.Kind != ScaffoldKind {
			continue
		}
		return fmt.Errorf("codegen.generators[%d]: %q is a scaffold generator: it writes code you own, once, and every build re-runs the codegen config. Run `gofastr generate %s` yourself instead; the config lists derived generators (%s) and extensions", i, gen.Name, gen.Name, derivedGeneratorNames())
	}
	return nil
}

func derivedGeneratorNames() string {
	var names []string
	for _, g := range Generators {
		if g.Kind == DerivedKind {
			names = append(names, g.Name)
		}
	}
	return strings.Join(names, ", ")
}
