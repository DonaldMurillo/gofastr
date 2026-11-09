package upgrade

import (
	"go/build"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRegistrySymbolsNameRealPackages: every symbol a note names lives
// in the gofastr module or the standard library. The parser expands
// only the "gofastr/" shorthand, so a note written "mcp.WithToolGate"
// names a package called mcp that no app imports: its uses matcher
// never fires, and the CI symbol gate, which skips packages outside the
// module, says nothing. Three v0.97.0 notes shipped that way (#669).
func TestRegistrySymbolsNameRealPackages(t *testing.T) {
	reg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	goroot := build.Default.GOROOT
	isStd := func(pkg string) bool {
		if goroot == "" || strings.Contains(strings.SplitN(pkg, "/", 2)[0], ".") {
			return false
		}
		st, err := os.Stat(filepath.Join(goroot, "src", filepath.FromSlash(pkg)))
		return err == nil && st.IsDir()
	}
	for _, rel := range reg.Releases {
		for _, n := range rel.Notes {
			var syms []Symbol
			syms = append(syms, n.Find.Uses...)
			for _, sm := range n.Find.Shapes {
				syms = append(syms, sm.Symbol)
			}
			for _, fm := range n.Find.Fields {
				syms = append(syms, fm.Field)
			}
			for _, s := range syms {
				if s.Pkg == ModulePath || strings.HasPrefix(s.Pkg, ModulePath+"/") || isStd(s.Pkg) {
					continue
				}
				t.Errorf("%s:%d (%s): symbol %s names package %q, neither in %s nor the standard library; spell gofastr/<path>.%s",
					n.File, n.Line, rel.Version, s, s.Pkg, ModulePath, s.Name)
			}
		}
	}
}
