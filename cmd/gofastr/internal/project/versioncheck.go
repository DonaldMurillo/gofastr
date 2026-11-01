package project

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/mod/semver"
)

// EnvNoVersionCheck turns VersionMismatch off when set to any non-empty
// value.
const EnvNoVersionCheck = "GOFASTR_NO_VERSION_CHECK"

// VersionMismatch is the warning to print when the project around dir
// requires a different GoFastr release than this CLI is, or "" when they
// match or there is nothing to compare: a dev build of the CLI, no
// enclosing go.mod, a go.mod that does not require GoFastr, the GoFastr
// module itself, or a replace directive (the version in go.mod is then
// not what builds). A project pinned to v0.95.0 run with a v0.94.1 CLI
// printed nothing before (#635): generated code and checks came from the
// older release.
func VersionMismatch(dir string) string {
	if os.Getenv(EnvNoVersionCheck) != "" {
		return ""
	}
	cliV := normalizeFrameworkVersion(CliVersion())
	if cliV == "" {
		return ""
	}
	mod, root := FindEnclosingGoMod(dir)
	if root == "" || mod == GofastrModule || strings.HasPrefix(mod, GofastrModule+"/") {
		return ""
	}
	modV, replaced, err := GoModGofastrVersion(root)
	if err != nil || replaced || modV == "" || !semver.IsValid(modV) {
		return ""
	}
	if semver.Compare(modV, cliV) == 0 {
		return ""
	}
	return fmt.Sprintf("this project requires %s %s but this gofastr CLI is %s; install the matching CLI with:\n  go install %s/cmd/gofastr@%s\n(set %s=1 to silence this check)",
		GofastrModule, modV, cliV, GofastrModule, modV, EnvNoVersionCheck)
}
