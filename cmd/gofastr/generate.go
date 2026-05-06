package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gofastr/gofastr/core/schema"
)

func runGenerate(args []string) {
	if len(args) < 2 {
		fail("Usage: gofastr generate entity <name> <field:definitions...>")
		info("Example: gofastr generate entity user name:string email:string:unique age:int")
		os.Exit(1)
	}

	resourceType := args[0]
	switch resourceType {
	case "entity":
		generateEntity(args[1:])
	default:
		fail("Unknown resource type: %s", resourceType)
		info("Supported: entity")
		os.Exit(1)
	}
}

func generateEntity(args []string) {
	if len(args) == 0 {
		fail("Entity name required.")
		info("Usage: gofastr generate entity <name> [field:definitions...]")
		os.Exit(1)
	}

	entityName := args[0]
	fieldDefs := args[1:]

	if len(fieldDefs) == 0 {
		fieldDefs = []string{"name:string"}
		info("No fields specified, using default: name:string")
	}

	// Parse field definitions: "name:type:flags"
	type fieldInfo struct {
		Name     string
		Type     schema.FieldType
		Required bool
		Unique   bool
	}
	var fields []fieldInfo

	for _, def := range fieldDefs {
		parts := strings.Split(def, ":")
		if len(parts) < 2 {
			fail("Invalid field definition: %s (expected name:type)", def)
			os.Exit(1)
		}

		fName := parts[0]
		fTypeStr := strings.ToLower(parts[1])
		required := false
		unique := false

		for _, flag := range parts[2:] {
			switch strings.ToLower(flag) {
			case "required":
				required = true
			case "unique":
				unique = true
			}
		}

		var fType schema.FieldType
		switch fTypeStr {
		case "string", "text":
			fType = schema.String
		case "int", "integer":
			fType = schema.Int
		case "float", "number":
			fType = schema.Float
		case "bool", "boolean":
			fType = schema.Bool
		case "date", "datetime", "timestamp":
			fType = schema.String // timestamps handled as strings for now
		case "enum":
			fType = schema.Enum
		default:
			fType = schema.String
			info("Unknown type %q, defaulting to string", fTypeStr)
		}

		fields = append(fields, fieldInfo{
			Name:     fName,
			Type:     fType,
			Required: required,
			Unique:   unique,
		})
	}

	// Generate Go file
	tableName := toSnakeCase(entityName)
	structName := toCamelCase(entityName)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(`package entities

import (
	"github.com/gofastr/gofastr/core/schema"
	"github.com/gofastr/gofastr/framework"
)

// %s is auto-generated. Edit freely.
func register%s(app *framework.App) {
	app.Entity("%s", framework.EntityConfig{
		Table: "%s",
		Fields: []schema.Field{
`, structName, structName, entityName, tableName))

	for _, f := range fields {
		sb.WriteString(fmt.Sprintf("\t\t\t{Name: %q, Type: schema.%s", f.Name, fieldTypeConst(f.Type)))
		if f.Required {
			sb.WriteString(", Required: true")
		}
		if f.Unique {
			sb.WriteString(", Unique: true")
		}
		sb.WriteString("},\n")
	}

	sb.WriteString(`		},
		CRUD: true,
	})
}
`)

	// Ensure entities directory exists
	entitiesDir := "entities"
	if _, err := os.Stat(entitiesDir); os.IsNotExist(err) {
		if err := os.MkdirAll(entitiesDir, 0o755); err != nil {
			fail("Failed to create entities directory: %v", err)
			os.Exit(1)
		}
	}

	filename := filepath.Join(entitiesDir, tableName+".go")
	if _, err := os.Stat(filename); err == nil {
		fail("File %s already exists. Remove it first or use a different name.", filename)
		os.Exit(1)
	}

	if err := os.WriteFile(filename, []byte(sb.String()), 0o644); err != nil {
		fail("Failed to write %s: %v", filename, err)
		os.Exit(1)
	}

	success("Generated entity %s → %s", bold(entityName), filename)
	info("Don't forget to call register%s(app) in your entities.go", structName)
}

func fieldTypeConst(ft schema.FieldType) string {
	switch ft {
	case schema.String:
		return "String"
	case schema.Int:
		return "Int"
	case schema.Float:
		return "Float"
	case schema.Bool:
		return "Bool"
	case schema.Enum:
		return "Enum"
	default:
		return "String"
	}
}

func toSnakeCase(s string) string {
	var result []byte
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			result = append(result, '_')
		}
		result = append(result, byte(strings.ToLower(string(r))[0]))
	}
	return string(result)
}

func toCamelCase(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == '_' || r == '-' || r == ' '
	})
	var result string
	for _, p := range parts {
		if len(p) > 0 {
			result += strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return result
}
