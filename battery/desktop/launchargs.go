package desktop

import (
	"encoding/base64"
	"encoding/json"
	"strings"
)

// builtLaunchArgs is set by `gofastr desktop build --launch-args`: the
// arguments, as base64url JSON, a double-clicked executable starts
// with. Empty when the build set none.
var builtLaunchArgs string

// builtLaunchScheme is the --scheme of a build that also set
// --launch-args, stamped on every platform so LaunchArgs can tell a
// deep link handed over as an argument. It is separate from
// builtDeepLinkScheme, which also sets the battery's default deep-link
// contract and which macOS builds leave to Info.plist.
var builtLaunchScheme string

// LaunchArgs returns the arguments main should act on, given its own
// (os.Args[1:]). A binary whose desktop mode is a subcommand ("myapp
// desktop") and whose no-argument mode is a server starts that server,
// windowless, when the OS launches it from an icon, because a
// double-click passes no arguments. Built with `gofastr desktop build
// --launch-args=desktop`, it calls
//
//	args := desktop.LaunchArgs(os.Args[1:])
//
// and gets ["desktop"] on a double-click. The baked arguments go in
// front when args is empty, when it is only the "-psn_..." process
// serial number older macOS Finder passes, or when every argument is a
// deep link on the scheme the build embedded (--scheme): the OS hands
// a cold-launch link to the executable as its argument. Any other
// arguments, and every argument of a build without --launch-args, come
// back unchanged.
func LaunchArgs(args []string) []string {
	baked := decodeLaunchArgs(builtLaunchArgs)
	if len(baked) == 0 {
		return args
	}
	rest := args
	if len(rest) == 1 && strings.HasPrefix(rest[0], "-psn_") {
		rest = nil
	}
	for _, a := range rest {
		if !isBuiltSchemeLink(a) {
			return args
		}
	}
	return append(baked, rest...)
}

// decodeLaunchArgs reverses the CLI's encoding; anything malformed is
// no baked arguments.
func decodeLaunchArgs(s string) []string {
	if s == "" {
		return nil
	}
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil
	}
	var out []string
	if err := json.Unmarshal(b, &out); err != nil {
		return nil
	}
	return out
}

// isBuiltSchemeLink reports whether a is a URL on the build's embedded
// deep-link scheme (builtDeepLinkScheme, or builtLaunchScheme on a
// build that stamped only that).
func isBuiltSchemeLink(a string) bool {
	low := strings.ToLower(a)
	for _, s := range []string{builtDeepLinkScheme, builtLaunchScheme} {
		if s != "" && strings.HasPrefix(low, s+":") {
			return true
		}
	}
	return false
}
