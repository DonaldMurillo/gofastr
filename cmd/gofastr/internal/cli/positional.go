package cli

import (
	"flag"
)

// ParsePositionals parses args through fs pass by pass, taking the first
// non-flag argument of each pass as a positional, so a positional may come
// before or after the flags. The first parse error is returned as is;
// flag.ErrHelp stays unwrapped so the caller can propagate it.
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
