// Package appid is the desktop app id grammar, which the desktop
// battery, gofastr desktop build and the Linux bundle all check: the id
// names a bundle identifier and one directory under the user's config
// and data directories.
package appid

import (
	"fmt"
	"regexp"
	"strings"
)

var pattern = regexp.MustCompile(`^[A-Za-z0-9.-]+$`)

// Check refuses an id that is not [A-Za-z0-9.-]+ with at least one dot,
// that is made only of dots and dashes, or that is longer than 253
// bytes. The error starts with the quoted id, for the caller to prefix
// with what the id is.
func Check(id string) error {
	if !pattern.MatchString(id) || !strings.Contains(id, ".") {
		return fmt.Errorf("%q must be reverse-DNS: [A-Za-z0-9.-]+ with at least one dot", id)
	}
	// ".", "..", "..." satisfy the grammar and the dot rule, and would
	// name a directory above the data dir.
	if strings.Trim(id, ".-") == "" {
		return fmt.Errorf("%q must not be a bare dot/dash sequence: it names a directory under the OS user config dir", id)
	}
	if len(id) > 253 {
		return fmt.Errorf("%q is longer than 253 characters", id)
	}
	return nil
}
