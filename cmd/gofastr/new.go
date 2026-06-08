package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// validateScaffoldName rejects names that would escape the project root.
// Scaffolding commands accept a short identifier (e.g. "User", "Post") and
// must never accept path traversal, absolute paths, or directory separators.
func validateScaffoldName(name string) error {
	if name == "" {
		return fmt.Errorf("name must not be empty")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("name %q is not a valid identifier", name)
	}
	if strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("name %q must not contain path separators", name)
	}
	if strings.Contains(name, "..") {
		return fmt.Errorf("name %q must not contain '..'", name)
	}
	if filepath.Base(name) != name {
		return fmt.Errorf("name %q must be a bare identifier", name)
	}
	return nil
}

// newUsage prints the resource list for `gofastr new`.
func newUsage() {
	fmt.Println("Usage: gofastr new <resource> <name> [flags]")
	fmt.Println("Resources:")
	fmt.Println("  handler  <Name> [--method=GET] [--path=/x] [-overwrite]   Scaffold an HTTP handler")
	fmt.Println("  route    <path>  [--method=GET] [--handler=Name]   Print a route registration snippet")
	fmt.Println("Flags:")
	fmt.Println("  -overwrite   Rewrite the target file if it already exists")
	fmt.Println("  -h, --help   Show this help")
}

// runNew handles the `gofastr new` subcommand — a lower-level scaffolding
// alternative to kiln's visual builder.
func runNew(args []string) {
	// `gofastr new -h` / `gofastr new --help` exits 0 with help.
	for _, a := range args {
		if a == "-h" || a == "--help" {
			newUsage()
			return
		}
	}

	if len(args) == 0 {
		newUsage()
		osExit(1)
	}

	resource := args[0]
	rest := args[1:]

	// Strip -overwrite from rest, leaving the resource-specific args.
	overwrite, rest := extractOverwriteFlag(rest)

	switch resource {
	case "entity":
		fail("`gofastr new entity` has been removed.")
		info("Declare entities in a gofastr.yml blueprint instead. See `gofastr docs blueprints`.")
		osExit(1)
	case "handler":
		runNewHandler(rest, overwrite)
	case "route":
		runNewRoute(rest)
	default:
		fail("Unknown resource: %s", resource)
		info("Supported: handler, route")
		osExit(1)
	}
}

// extractOverwriteFlag scans args for -overwrite / --overwrite and returns
// (true, argsWithoutFlag) if present.
func extractOverwriteFlag(args []string) (bool, []string) {
	out := args[:0:0]
	overwrite := false
	for _, a := range args {
		if a == "-overwrite" || a == "--overwrite" {
			overwrite = true
			continue
		}
		out = append(out, a)
	}
	return overwrite, out
}

// runNewHandler is the CLI wrapper around scaffoldHandler.
func runNewHandler(args []string, overwrite bool) {
	if len(args) == 0 {
		fail("Usage: gofastr new handler <Name> --method <GET|POST> --path <path>")
		osExit(1)
	}

	name := args[0]
	method := "GET"
	path := "/" + strings.ToLower(name)

	for _, a := range args[1:] {
		if strings.HasPrefix(a, "--method=") {
			method = strings.ToUpper(strings.TrimPrefix(a, "--method="))
		} else if strings.HasPrefix(a, "--path=") {
			path = strings.TrimPrefix(a, "--path=")
		}
	}

	if err := scaffoldHandler(".", name, method, path, overwrite); err != nil {
		fail("%v", err)
		osExit(1)
	}
	success("Scaffolded handler %q (%s %s)", name, method, path)
}

// runNewRoute is the CLI wrapper that prints a route registration snippet.
func runNewRoute(args []string) {
	if len(args) == 0 {
		fail("Usage: gofastr new route <path> --method <GET|POST> --handler <name>")
		osExit(1)
	}

	path := args[0]
	method := "GET"
	handler := "handler"

	for _, a := range args[1:] {
		if strings.HasPrefix(a, "--method=") {
			method = strings.ToUpper(strings.TrimPrefix(a, "--method="))
		} else if strings.HasPrefix(a, "--handler=") {
			handler = strings.TrimPrefix(a, "--handler=")
		}
	}

	info("Add this to your app setup:")
	fmt.Printf("\n  %s\n\n", routeSnippet(method, path, handler))
}

// routeSnippet returns the canonical app.Router().Handle(...) line for
// the given route. Pure helper; used by both runNewRoute and golden tests.
func routeSnippet(method, path, handler string) string {
	return fmt.Sprintf("app.Router().Handle(%q, %q, %s)", method, path, handler)
}

// scaffoldHandler writes an HTTP handler file at <baseDir>/<name>_handler.go.
// Returns an error when the file already exists and overwrite is false.
func scaffoldHandler(baseDir, rawName, method, path string, overwrite bool) error {
	if err := validateScaffoldName(rawName); err != nil {
		return fmt.Errorf("invalid handler name: %w", err)
	}
	name := rawName
	filename := filepath.Join(baseDir, strings.ToLower(name)+"_handler.go")
	if !overwrite {
		if _, err := os.Stat(filename); err == nil {
			return fmt.Errorf("handler file already exists: %s", filename)
		}
	}

	content := fmt.Sprintf(`// %s handler — scaffolded by gofastr new handler.
package main

import (
    "net/http"
)

// %s handles %s %s.
func %s(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusOK)
    w.Write([]byte("{\"ok\": true}"))
}
`, name, name, method, path, name)

	if err := os.WriteFile(filename, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", filename, err)
	}
	return nil
}
