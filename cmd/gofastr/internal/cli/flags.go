package cli

import (
	"flag"
	"fmt"
	"slices"
	"strings"
)

// ParseFlags reads --name=value and --name value flags and returns those it saw.
func ParseFlags(args []string, valued map[string]*string, bare map[string]*bool, validators ...func(name, value string) error) (map[string]bool, error) {
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
			if i+1 >= len(args) || IsFlagToken(args[i+1]) {
				return seen, NeedsValue(name)
			}
			i++
			value = args[i]
		}
		for _, validate := range validators {
			if err := validate(name, value); err != nil {
				return seen, err
			}
		}
		*dst = value
		seen[name] = true
	}
	return seen, nil
}

// IsFlagToken reports whether s starts with a dash and is not a lone "-".
func IsFlagToken(s string) bool { return len(s) > 1 && s[0] == '-' }

// NeedsValue is the refusal for a valued flag with no value after it.
func NeedsValue(name string) error {
	return fmt.Errorf("%s needs a value (write %s=<value> for one that starts with a dash)", name, name)
}

// ParsePositionals parses args through fs and returns the non-flag arguments.
func ParsePositionals(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for len(args) > 0 {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) > 0 {
			positional = append(positional, args[0])
			args = args[1:]
		}
	}
	return positional, nil
}

// HasHelpFlag reports whether --help or -h appears anywhere in args.
func HasHelpFlag(args []string) bool {
	for _, arg := range args {
		if arg == "--help" || arg == "-h" {
			return true
		}
	}
	return false
}

// HasHelpFlagBeforeTerminator is HasHelpFlag over the arguments before the first "--".
func HasHelpFlagBeforeTerminator(args []string) bool {
	if end := slices.Index(args, "--"); end >= 0 {
		args = args[:end]
	}
	return HasHelpFlag(args)
}

// HasFlagBeforeTerminator reports whether flag appears in args before the first "--".
func HasFlagBeforeTerminator(args []string, flag string) bool {
	if end := slices.Index(args, "--"); end >= 0 {
		args = args[:end]
	}
	return slices.Contains(args, flag)
}
