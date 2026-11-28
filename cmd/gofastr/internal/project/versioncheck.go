package project

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/mod/semver"
)

// EnvNoVersionCheck, set to any value, turns VersionMismatch off.
const EnvNoVersionCheck = "GOFASTR_NO_VERSION_CHECK"

// VersionMismatch warns when dir's project requires another GoFastr release than this CLI, else "".
func VersionMismatch(dir string) string {
	if os.Getenv(EnvNoVersionCheck) != "" {
		return ""
	}
	cliV := InstalledFrameworkVersion()
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
