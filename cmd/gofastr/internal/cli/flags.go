package cli

import (
	"fmt"
	"strings"
)

// ParseFlags reads hand-rolled flags in both the --name=value and the
// --name value form. valued maps a flag to where its value goes and bare
// maps a flag that takes none to the bool it sets; it returns the flags
// it saw.
//
// It refuses what a typo or a missing value would otherwise turn into a
// silent default: an unknown flag, an argument that is not a flag, a bare
// flag given =value, and a valued flag whose next argument is the next
// flag (`--addr --no-a11y` took "--no-a11y" as the address and dropped
// the a11y skip). A value that starts with a dash is written
// --name=<value>; a lone "-" (codesign's ad-hoc identity) is a value.
func ParseFlags(args []string, valued map[string]*string, bare map[string]*bool) (map[string]bool, error) {
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		name, value, hasValue := strings.Cut(args[i], "=")
		if !strings.HasPrefix(name, "-") || name == "-" {
			return seen, fmt.Errorf("unexpected argument %q", args[i])
		}
		if b, ok := bare[name]; ok {
			if hasValue {
				return seen, fmt.Errorf("%s takes no value", name)
			}
			*b = true
			seen[name] = true
			continue
		}
		dst, ok := valued[name]
		if !ok {
			return seen, fmt.Errorf("unknown flag %s", name)
		}
		if !hasValue {
			if i+1 >= len(args) || isFlagToken(args[i+1]) {
				return seen, fmt.Errorf("%s needs a value (write %s=<value> for one that starts with a dash)", name, name)
			}
			i++
			value = args[i]
		}
		*dst = value
		seen[name] = true
	}
	return seen, nil
}

func isFlagToken(s string) bool { return len(s) > 1 && s[0] == '-' }
