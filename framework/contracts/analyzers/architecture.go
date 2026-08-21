package analyzers

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

func init() {
	contracts.Register(&contracts.Analyzer{
		Name: "architecture",
		Doc:  "Dependency direction: layer ordering and explicitly forbidden import edges.",
		Rules: []string{
			contracts.RuleLayerViolation,
			contracts.RuleForbiddenImport,
		},
		Run: runArchitecture,
	})
}

// runArchitecture enforces the dependency shape the project declared.
//
// It stays completely silent when nothing is declared. That is the only
// honest default: a layering the tool invented for someone else's package
// tree would be wrong more often than right, and a wrong architecture
// rule is worse than none. It trains people to ignore the analyzer.
func runArchitecture(p *contracts.Pass) ([]contracts.Diagnostic, error) {
	arch := p.Config.Architecture
	if !arch.Configured() {
		return nil, nil
	}

	// Layer index by name, and a resolver from import path to layer. The
	// first matching layer wins, so an earlier, more specific entry can
	// carve an exception out of a later `**` catch-all.
	layerOf := func(importPath string) (int, string, bool) {
		for i, layer := range arch.Layers {
			for _, pattern := range layer.Packages {
				if matchImportPath(pattern, importPath, p.ModulePath) {
					return i, layer.Name, true
				}
			}
		}
		return 0, "", false
	}

	var out []contracts.Diagnostic
	for _, f := range p.AppFiles() {
		file, ok := p.AST(f.Rel)
		if !ok {
			continue
		}
		fromIdx, fromName, fromKnown := layerOf(f.Package)
		for _, imp := range file.Imports {
			target, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				continue
			}

			for _, forbid := range arch.Forbid {
				if !matchImportPath(forbid.From, f.Package, p.ModulePath) ||
					!matchImportPath(forbid.To, target, p.ModulePath) {
					continue
				}
				msg := fmt.Sprintf("%s must not import %s", f.Package, target)
				d := diag(p, contracts.RuleForbiddenImport, f.Rel, imp.Pos(), msg)
				if forbid.Reason != "" {
					d.Suggestion = forbid.Reason
				}
				d.Evidence = map[string]string{"from": f.Package, "to": target}
				out = append(out, d)
			}

			if !fromKnown {
				continue
			}
			toIdx, toName, toKnown := layerOf(target)
			// Layers are ordered top-first, so a *lower* index is a higher
			// layer. An import is upward, and forbidden, when the target
			// sits at a smaller index than the importer.
			if !toKnown || toIdx >= fromIdx {
				continue
			}
			d := diag(p, contracts.RuleLayerViolation, f.Rel, imp.Pos(), fmt.Sprintf(
				"%s (layer %q) imports %s (layer %q): %q sits above %q",
				f.Package, fromName, target, toName, toName, fromName))
			d.Evidence = map[string]string{
				"from": f.Package, "fromLayer": fromName,
				"to": target, "toLayer": toName,
			}
			out = append(out, d)
		}
	}
	return out, nil
}

// matchImportPath matches a glob against an import path, trying the full
// path and then the module-relative one. That lets a config say
// `core/**` instead of repeating `github.com/you/app/` in front of every
// entry, while still allowing a full path for an external dependency.
//
// It deliberately does NOT try every suffix. That was the first
// implementation and it is subtly wrong: for a module named
// `github.com/acme/core`, the suffix `core` matches the glob `core/**`,
// so *every* package in the module lands in the core layer and the rule
// silently stops finding anything. Stripping the known module prefix is
// the precise version of the same convenience.
func matchImportPath(pattern, importPath, modulePath string) bool {
	if matchImportGlob(pattern, importPath) {
		return true
	}
	if modulePath == "" {
		return false
	}
	if importPath == modulePath {
		// The module root package itself, whose relative path is "".
		return false
	}
	rel, ok := strings.CutPrefix(importPath, modulePath+"/")
	return ok && matchImportGlob(pattern, rel)
}

// matchImportGlob is contracts' path matcher applied to an import path.
// Import paths are already slash-separated, so the dialect carries over
// unchanged: `*` within a segment, `**` across segments.
func matchImportGlob(pattern, path string) bool {
	return contracts.MatchPath(pattern, path)
}
