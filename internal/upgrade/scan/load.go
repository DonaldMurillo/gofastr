package scan

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/tools/go/packages"
)

// findModuleRoot walks up from dir to the filesystem root and returns the
// first ancestor holding a go.mod. os.Root.Stat confines each probe to the
// directory it names.
func findModuleRoot(dir string) (string, error) {
	for {
		r, err := os.OpenRoot(dir)
		if err != nil {
			return "", fmt.Errorf("open %s: %w", dir, err)
		}
		_, statErr := r.Stat("go.mod")
		r.Close()
		if statErr == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("scan: no go.mod at or above %s", dir)
		}
		dir = parent
	}
}

// loadPackages loads the app's packages once: one packages.Load against
// the module at or above root, patterns scoped to root, tests included.
// Dependencies come back as type information, not scan targets; generated
// files are the app's code and load with it. The go tool inherits the
// process environment, so GOFLAGS and GOWORK are respected.
func loadPackages(root, moduleRoot string) ([]*packages.Package, error) {
	pattern := "./..."
	if rel, err := filepath.Rel(moduleRoot, root); err == nil && rel != "." {
		pattern = "./" + filepath.ToSlash(rel) + "/..."
	}
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo |
			packages.NeedImports | packages.NeedModule,
		Dir:   moduleRoot,
		Tests: true,
	}
	return packages.Load(cfg, pattern)
}
