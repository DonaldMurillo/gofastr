package desktop

import (
	"encoding/base64"
	"encoding/json"
	"strings"
)

var builtLaunchArgs string

var builtLaunchScheme string

// LaunchArgs returns the arguments main should act on, given its own (os.Args[1:]).
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

func isBuiltSchemeLink(a string) bool {
	low := strings.ToLower(a)
	for _, s := range []string{builtDeepLinkScheme, builtLaunchScheme} {
		if s != "" && strings.HasPrefix(low, s+":") {
			return true
		}
	}
	return false
}
