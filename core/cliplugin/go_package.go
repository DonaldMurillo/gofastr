package cliplugin

import (
	"path/filepath"
	"strings"

	"golang.org/x/mod/module"
)

func validGoPackage(path string) bool {
	return path != "all" && path != "std" && path != "cmd" &&
		!strings.Contains(path, "...") && module.CheckImportPath(path) == nil
}

func projectGoBinary(name string) string {
	return filepath.Join(".gofastr", "plugins", name+".exe")
}
