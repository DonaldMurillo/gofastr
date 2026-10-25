package upgradecmd

import (
	"fmt"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/project"
	"golang.org/x/mod/semver"
)

// printUnmountedForUpgrade lists the copies an upgrade does not touch,
// each older one with its upstream diff to the target.
func printUnmountedForUpgrade(root, target string) {
	_, entries, err := project.UnmountedSettings(root)
	if err != nil {
		fmt.Printf("NOTE: could not read the unmounted packages: %v\n\n", err)
		return
	}
	if len(entries) == 0 {
		return
	}
	fmt.Println("Unmounted packages (upgrade never changes them; carry what you need by hand):")
	for _, e := range entries {
		fmt.Printf("  %s\n", project.DescribeUnmounted(e))
		switch {
		case project.UnmountedBehind(e.Version, target):
			fmt.Printf("    changes upstream since: %s\n", project.UnmountedCompareURL(e.Version, target))
		case !project.IsReleaseTag(e.Version) || !semver.IsValid(e.Version):
			fmt.Printf("    copied from a checkout, not a release: compare it with %s by hand\n", target)
		}
	}
	fmt.Println()
}
