package main

import (
	"errors"
	"strings"
)

type buildOptions struct {
	output     string
	pkg        string
	noGenerate bool
	noA11y     bool
}

func parseBuildOptions(args []string) (buildOptions, error) {
	opts := buildOptions{output: "bin/server", pkg: "."}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--no-generate":
			opts.noGenerate = true
		case arg == "--no-a11y":
			opts.noA11y = true
		case arg == "--pkg":
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				return buildOptions{}, errors.New("build: --pkg requires a package path")
			}
			i++
			opts.pkg = args[i]
			if strings.HasPrefix(opts.pkg, "-") {
				return buildOptions{}, errors.New("build: --pkg requires a package path, not a Go option")
			}
		case strings.HasPrefix(arg, "--pkg="):
			opts.pkg = strings.TrimPrefix(arg, "--pkg=")
			if opts.pkg == "" || strings.HasPrefix(opts.pkg, "-") {
				return buildOptions{}, errors.New("build: --pkg requires a package path")
			}
		case strings.HasPrefix(arg, "-o="):
			opts.output = strings.TrimPrefix(arg, "-o=")
		case strings.HasPrefix(arg, "--output="):
			opts.output = strings.TrimPrefix(arg, "--output=")
		case strings.HasPrefix(arg, "-"):
			return buildOptions{}, errors.New("build: unknown option " + arg)
		default:
			return buildOptions{}, errors.New("build: unexpected argument " + arg)
		}
	}
	return opts, nil
}
