package analyzers

import (
	"fmt"
	"go/ast"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

func init() {
	contracts.Register(&contracts.Analyzer{
		Name: "guidance",
		Doc:  "Hand-rolled code where a framework primitive already exists.",
		Rules: []string{
			contracts.RuleHandrolledCRUD,
			contracts.RuleHandrolledBattery,
			contracts.RuleRawSQLOverRepo,
		},
		Run: runGuidance,
	})
}

func runGuidance(p *contracts.Pass) ([]contracts.Diagnostic, error) {
	var out []contracts.Diagnostic
	out = append(out, handrolledCRUD(p)...)
	out = append(out, handrolledBattery(p)...)
	out = append(out, rawSQLOverRepo(p)...)
	return out, nil
}

// handrolledCRUD reports a resource whose REST surface was written by
// hand. The bar is deliberately high, three or more of the five CRUD
// verbs on one collection path, because two endpoints on a resource is
// a normal amount of custom behaviour, and five is someone reimplementing
// `app.Entity`.
func handrolledCRUD(p *contracts.Pass) []contracts.Diagnostic {
	table := Routes(p)
	if !table.Registered {
		return nil
	}
	declared := map[string]bool{}
	for _, e := range Entities(p) {
		declared[strings.ToLower(e.Name)] = true
	}

	type resource struct {
		verbs map[string]bool
		first Route
	}
	byResource := map[string]*resource{}
	var order []string
	for _, r := range table.Routes {
		collection := collectionPath(r.Pattern)
		if collection == "" {
			continue
		}
		res, ok := byResource[collection]
		if !ok {
			res = &resource{verbs: map[string]bool{}, first: r}
			byResource[collection] = res
			order = append(order, collection)
		}
		res.verbs[strings.ToUpper(r.Method)] = true
	}

	var out []contracts.Diagnostic
	for _, collection := range order {
		res := byResource[collection]
		crud := 0
		for _, verb := range []string{"GET", "POST", "PUT", "PATCH", "DELETE"} {
			if res.verbs[verb] {
				crud++
			}
		}
		if crud < 3 {
			continue
		}
		// A resource that already has a declared entity is fine. The
		// hand-written routes are the custom operations beside it.
		name := strings.Trim(collection, "/")
		if i := strings.LastIndex(name, "/"); i >= 0 {
			name = name[i+1:]
		}
		if declared[strings.ToLower(name)] {
			continue
		}
		verbs := make([]string, 0, len(res.verbs))
		for v := range res.verbs {
			verbs = append(verbs, v)
		}
		sort.Strings(verbs)
		d := diag(p, contracts.RuleHandrolledCRUD, res.first.File, res.first.Pos, fmt.Sprintf(
			"%s has hand-written %s handlers and no declared entity", collection, strings.Join(verbs, "/")))
		d.Suggestion = fmt.Sprintf(
			"declare the entity: app.Entity(%q, …) mounts the same surface plus filtering, pagination, includes, OpenAPI, and MCP tools", name)
		d.Evidence = map[string]string{"collection": collection, "methods": strings.Join(verbs, ",")}
		out = append(out, d)
	}
	return out
}

// collectionPath reduces "/api/orders/{id}" to "/api/orders" so the item
// and collection routes of one resource group together. Returns "" for a
// path with no usable resource segment.
func collectionPath(pattern string) string {
	segments := strings.Split(strings.Trim(pattern, "/"), "/")
	if len(segments) == 0 || segments[0] == "" {
		return ""
	}
	// Drop a trailing parameter segment, and any action verb after it
	// ("/orders/{id}/confirm" is still the orders resource).
	for len(segments) > 0 {
		last := segments[len(segments)-1]
		if strings.HasPrefix(last, "{") || strings.HasPrefix(last, ":") {
			segments = segments[:len(segments)-1]
			continue
		}
		break
	}
	if len(segments) == 0 {
		return ""
	}
	// Framework-internal surfaces are not the user's resources.
	joined := "/" + strings.Join(segments, "/")
	for _, reserved := range []string{"/__gofastr", "/mcp", "/health", "/metrics", "/.well-known"} {
		if strings.HasPrefix(joined, reserved) {
			return ""
		}
	}
	return joined
}

// batterySignals maps a stdlib or third-party import that indicates a
// hand-rolled subsystem onto the battery that already provides it.
var batterySignals = []struct {
	Imports []string
	Battery string
	What    string
}{
	{[]string{"net/smtp"}, "battery/email", "transactional email"},
	{[]string{"golang.org/x/crypto/bcrypt", "golang.org/x/crypto/argon2", "golang.org/x/crypto/scrypt"},
		"battery/auth", "password hashing and session management"},
	{[]string{"github.com/aws/aws-sdk-go-v2/service/s3", "github.com/minio/minio-go/v7"},
		"battery/storage", "file storage with signed URLs"},
	{[]string{"github.com/robfig/cron", "github.com/robfig/cron/v3", "github.com/go-co-op/gocron"},
		"framework/cron", "scheduled jobs"},
}

func handrolledBattery(p *contracts.Pass) []contracts.Diagnostic {
	var out []contracts.Diagnostic
	for _, f := range p.AppFiles() {
		// The batteries themselves obviously use these packages.
		if strings.HasPrefix(f.Rel, "battery/") || strings.HasPrefix(f.Rel, "framework/") {
			continue
		}
		file, ok := p.AST(f.Rel)
		if !ok {
			continue
		}
		for _, signal := range batterySignals {
			if !importsAny(file, signal.Imports...) {
				continue
			}
			// Already using the battery alongside it. That is composition,
			// not reimplementation.
			if importsAny(file, signal.Battery) {
				continue
			}
			d := diag(p, contracts.RuleHandrolledBattery, f.Rel, file.Pos(), fmt.Sprintf(
				"this file implements %s directly: %s already provides it", signal.What, signal.Battery))
			d.Suggestion = fmt.Sprintf("register %s and keep the custom parts in a hook", signal.Battery)
			d.Evidence = map[string]string{"battery": signal.Battery, "subsystem": signal.What}
			out = append(out, d)
		}
	}
	return out
}

// rawSQLOverRepo reports a raw query naming a table that has a declared
// entity. The entity's scoping, whether soft delete, tenant filter, or
// owner field, lives in the CRUD layer, so a raw query silently returns
// rows those rules exist to hide.
func rawSQLOverRepo(p *contracts.Pass) []contracts.Diagnostic {
	entities := Entities(p)
	if len(entities) == 0 {
		return nil
	}
	tables := map[string]string{}
	for _, e := range entities {
		tables[strings.ToLower(e.Name)] = e.Name
	}

	var out []contracts.Diagnostic
	for _, f := range p.AppFiles() {
		file, ok := p.AST(f.Rel)
		if !ok {
			continue
		}
		lines := p.Lines(f.Rel)
		ast.Inspect(file, func(n ast.Node) bool {
			recv, method, call, isCall := selectorCall(n)
			if !isCall || !queryMethods[method] || len(call.Args) == 0 {
				return true
			}
			for _, arg := range call.Args {
				sql, litOK := stringLit(arg)
				if !litOK {
					continue
				}
				table := tableInStatement(sql, tables)
				if table == "" {
					continue
				}
				pos := p.Position(call.Pos())
				if hasLegacyAnnotation(lines, pos.Line, "safe-sql:", "reporting-query:") {
					return true
				}
				d := diag(p, contracts.RuleRawSQLOverRepo, f.Rel, call.Pos(), fmt.Sprintf(
					"raw %s against %q, which is a declared entity: the entity's scoping does not apply here",
					method, table))
				d.Evidence = map[string]string{"table": table, "receiver": exprText(recv)}
				out = append(out, d)
				return true
			}
			return true
		})
	}
	return out
}

// tableInStatement finds a declared entity name used as a table in a SQL
// statement: after FROM, INTO, UPDATE, or JOIN. Matching only in those
// positions keeps a column or alias that happens to share the name from
// counting.
func tableInStatement(sql string, tables map[string]string) string {
	fields := strings.FieldsFunc(strings.ToLower(sql), func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '(' || r == ')' || r == ',' || r == ';'
	})
	for i, word := range fields {
		switch word {
		case "from", "into", "update", "join":
			if i+1 < len(fields) {
				candidate := strings.Trim(fields[i+1], `"'`+"`")
				if name, ok := tables[candidate]; ok {
					return name
				}
			}
		}
	}
	return ""
}
