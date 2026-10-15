package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type exportOptions struct {
	pkg, base, dir string
}

func parseExportOptions(args []string) (exportOptions, error) {
	opts := exportOptions{pkg: "."}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		var name, value string
		switch {
		case arg == "--pkg" || arg == "--base":
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				return exportOptions{}, fmt.Errorf("export: %s needs a value", arg)
			}
			name, value = arg, args[i+1]
			i++
		case strings.HasPrefix(arg, "--pkg="), strings.HasPrefix(arg, "--base="):
			name, value, _ = strings.Cut(arg, "=")
		case strings.HasPrefix(arg, "-"):
			return exportOptions{}, errors.New("export: unknown option " + arg)
		default:
			if opts.dir != "" {
				return exportOptions{}, errors.New("export: unexpected argument " + arg)
			}
			opts.dir = arg
			continue
		}
		switch name {
		case "--pkg":
			if value == "" || strings.HasPrefix(value, "-") {
				return exportOptions{}, errors.New("export: --pkg needs a package path")
			}
			opts.pkg = value
		case "--base":
			if value != "" && (!strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/")) {
				return exportOptions{}, fmt.Errorf("export: --base %q must start with / and not end with one (\"/docs\")", value)
			}
			opts.base = value
		}
	}
	if opts.dir == "" {
		return exportOptions{}, errors.New("export: name the output directory")
	}
	return opts, nil
}

// runExport writes the app as a static site: it runs the app's main
// package with GOFASTR_EXPORT set, and App.Start renders every page
// (dynamic routes through StaticPaths) into the directory and returns
// instead of serving. A battery that adds export files, such as
// battery/sitesearch's index, writes them too.
func runExport(args []string) {
	if hasHelpFlag(args) {
		printExportUsage()
		return
	}
	opts, err := parseExportOptions(args)
	if err != nil {
		fail("%v", err)
		osExit(1)
		return
	}
	if err := validateBuildTarget(opts.pkg); err != nil {
		fail("Export target %q is invalid: %v", opts.pkg, err)
		osExit(1)
		return
	}
	dir, err := filepath.Abs(opts.dir)
	if err != nil {
		fail("export: %v", err)
		osExit(1)
		return
	}
	cmd := exec.Command("go", "run", opts.pkg)
	cmd.Env = append(os.Environ(), "GOFASTR_EXPORT="+dir, "GOFASTR_EXPORT_BASE="+opts.base)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		fail("export: the app exited with %v", err)
		osExit(1)
		return
	}
	success("Exported to %s", dir)
}

func printExportUsage() {
	fmt.Println(`Usage: gofastr export [--pkg=<path>] [--base=<subpath>] <dir>

Renders every page of the app into <dir> as a static site: it runs the
app's main package with GOFASTR_EXPORT set, and App.Start exports
instead of serving. Dynamic routes export through StaticPaths; a
battery that adds export files (battery/sitesearch's index) writes them
too. The app's main must return once Start does.

--pkg=<path>      the app's main package (default .)
--base=<subpath>  URL subpath the site is served under ("/docs"); default none`)
}
