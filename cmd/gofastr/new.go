package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// runNew handles the `gofastr new` subcommand — a lower-level scaffolding
// alternative to kiln's visual builder.
func runNew(args []string) {
	if len(args) == 0 {
		fail("Usage: gofastr new <resource> <name> [flags]")
		info("Resources: entity, handler, route")
		os.Exit(1)
	}

	resource := args[0]
	rest := args[1:]

	switch resource {
	case "entity":
		newEntity(rest)
	case "handler":
		newHandler(rest)
	case "route":
		newRoute(rest)
	default:
		fail("Unknown resource: %s", resource)
		info("Supported: entity, handler, route")
		os.Exit(1)
	}
}

// newEntity scaffolds a new entity JSON declaration.
func newEntity(args []string) {
	if len(args) == 0 {
		fail("Usage: gofastr new entity <Name> [field:type ...]")
		os.Exit(1)
	}

	name := args[0]
	fieldArgs := args[1:]
	name = strings.Title(strings.ToLower(name))

	// Parse fields into schema.Field list
	var fields []string
	for _, fa := range fieldArgs {
		field := parseFieldArg(fa)
		if field != "" {
			fields = append(fields, field)
		}
	}

	if len(fields) == 0 {
		fields = []string{`{"name": "name", "type": "string"}`}
	}

	tableName := strings.ToLower(name) + "s"
	generateEntityJSON(name, tableName, fields)
	success("Scaffolded entity %q with %d fields", name, len(fields))
	info("Run 'gofastr generate entity' to update the codegen")
}

// newHandler scaffolds a new HTTP handler file.
func newHandler(args []string) {
	if len(args) == 0 {
		fail("Usage: gofastr new handler <Name> --method <GET|POST> --path <path>")
		os.Exit(1)
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

	filename := strings.ToLower(name) + "_handler.go"
	if _, err := os.Stat(filename); err == nil {
		fail("Handler file already exists: %s", filename)
		os.Exit(1)
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
		fail("Failed to write %s: %v", filename, err)
		os.Exit(1)
	}
	success("Scaffolded handler %q (%s %s)", name, method, path)
}

// newRoute shows a route registration snippet.
func newRoute(args []string) {
	if len(args) == 0 {
		fail("Usage: gofastr new route <path> --method <GET|POST> --handler <name>")
		os.Exit(1)
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
	fmt.Printf("\n  app.Router().Handle(%q, %q, %s)\n\n", method, path, handler)
}

// parseFieldArg parses "name:type" or "name:type:modifier" into a JSON field def.
func parseFieldArg(arg string) string {
	parts := strings.Split(arg, ":")
	if len(parts) < 2 {
		return fmt.Sprintf(`{"name": "%s", "type": "string"}`, parts[0])
	}

	name := parts[0]
	typeStr := parts[1]
	ft := schemaFieldType(typeStr)

	field := fmt.Sprintf(`{"name": "%s", "type": "%s"`, name, ft)
	for _, mod := range parts[2:] {
		switch strings.ToLower(mod) {
		case "unique":
			field += `, "unique": true`
		case "required":
			field += `, "required": true`
		}
	}
	field += "}"
	return field
}

// schemaFieldType maps a CLI type string to a schema FieldType string.
func schemaFieldType(s string) string {
	switch strings.ToLower(s) {
	case "string", "text":
		return "string"
	case "int", "integer":
		return "int"
	case "float", "float64", "decimal":
		return "float"
	case "bool", "boolean":
		return "bool"
	case "datetime", "timestamp", "time":
		return "datetime"
	case "date":
		return "date"
	case "json", "jsonb":
		return "json"
	case "uuid":
		return "uuid"
	case "blob", "bytes":
		return "blob"
	default:
		return "string"
	}
}

// generateEntityJSON writes a JSON entity declaration file.
func generateEntityJSON(name, tableName string, fields []string) {
	dir := "entities"
	os.MkdirAll(dir, 0o755)

	filename := filepath.Join(dir, tableName+".json")
	if _, err := os.Stat(filename); err == nil {
		fail("Entity file already exists: %s", filename)
		os.Exit(1)
	}

	content := fmt.Sprintf(`{
  "name": "%s",
  "table": "%s",
  "fields": [
    %s
  ]
}`, name, tableName, strings.Join(fields, ",\n    "))

	if err := os.WriteFile(filename, []byte(content), 0o644); err != nil {
		fail("Failed to write %s: %v", filename, err)
		os.Exit(1)
	}
	success("Created %s", filename)
}
