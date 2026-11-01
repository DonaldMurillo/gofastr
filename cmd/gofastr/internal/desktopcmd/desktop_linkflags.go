package desktopcmd

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// builtLaunchArgsVar is the battery/desktop variable --launch-args
// stamps; desktop.LaunchArgs reads it.
const builtLaunchArgsVar = "github.com/DonaldMurillo/gofastr/battery/desktop.builtLaunchArgs"

const (
	maxLaunchArgs     = 16
	maxLaunchArgBytes = 256
)

// parseLaunchArgs splits --launch-args on whitespace and refuses what
// would not survive the trip into argv: a control byte, invalid UTF-8,
// an empty or oversized list. A value is one or more words ("desktop",
// "desktop --profile=work"); a single argument holding a space is not
// expressible.
func parseLaunchArgs(s string) ([]string, error) {
	if s == "" {
		return nil, nil
	}
	if !utf8.ValidString(s) {
		return nil, fmt.Errorf("--launch-args is not valid UTF-8")
	}
	for _, r := range s {
		if r != ' ' && r != '\t' && (r < 0x20 || r == 0x7f) {
			return nil, fmt.Errorf("--launch-args holds a control character")
		}
	}
	args := strings.Fields(s)
	if len(args) == 0 {
		return nil, fmt.Errorf("--launch-args is empty")
	}
	if len(args) > maxLaunchArgs {
		return nil, fmt.Errorf("--launch-args takes at most %d arguments", maxLaunchArgs)
	}
	for _, a := range args {
		if len(a) > maxLaunchArgBytes {
			return nil, fmt.Errorf("--launch-args: an argument is longer than %d bytes", maxLaunchArgBytes)
		}
	}
	return args, nil
}

// extraLinkerFlags is what --launch-args and --ldflags add to a build's
// own linker flags. The launch arguments travel as base64url JSON, so
// no quoting inside -ldflags is needed; --ldflags goes last, verbatim
// (the go command parses its quoting).
func extraLinkerFlags(f desktopBuildFlags) (string, error) {
	var extra string
	args, err := parseLaunchArgs(f.launchArgs)
	if err != nil {
		return "", err
	}
	if len(args) > 0 {
		b, err := json.Marshal(args)
		if err != nil {
			return "", err
		}
		extra += " -X " + builtLaunchArgsVar + "=" + base64.RawURLEncoding.EncodeToString(b)
	}
	if f.ldflags != "" {
		for _, r := range f.ldflags {
			if r < 0x20 || r == 0x7f {
				return "", fmt.Errorf("--ldflags holds a control character")
			}
		}
		extra += " " + f.ldflags
	}
	return extra, nil
}
