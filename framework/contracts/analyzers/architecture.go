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

func runArchitecture(p *contracts.Pass) ([]contracts.Diagnostic, error) {
	arch := p.Config.Architecture
	if !arch.Configured() {
		return nil, nil
	}

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

func matchImportPath(pattern, importPath, modulePath string) bool {
	if matchImportGlob(pattern, importPath) {
		return true
	}
	if modulePath == "" {
		return false
	}
	if importPath == modulePath {
		return false
	}
	rel, ok := strings.CutPrefix(importPath, modulePath+"/")
	return ok && matchImportGlob(pattern, rel)
}

func matchImportGlob(pattern, path string) bool {
	return contracts.MatchPath(pattern, path)
}
