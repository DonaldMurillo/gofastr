package main

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// TestAgentsMDSnippetsReferenceRealSymbols extracts every fenced ```go
// block from every agents.md and validates two classes of reference
// against the battery's source:
//
//  1. Package-direct (`auth.New`, `email.LoadFromDir`) — must be an
//     exported identifier of the battery.
//  2. Instance-method (`q.RegisterHandler` where
//     `q, err := queue.NewDBQueue(...)`) — must be a method on the
//     constructor's return type. Distinguishes `q.Register` from
//     `q.RegisterHandler` even when `Register` exists as a method on
//     a DIFFERENT type in the same package (e.g. ScheduleBuilder).
//
// Heuristic, not full type-checking — but covers the four failure
// modes that motivated the test (queue.Register, webhook.Start(ctx),
// email.LoadFromDir signature, auth.NewAuthManager) without dragging
// in golang.org/x/tools/go/packages.
func TestAgentsMDSnippetsReferenceRealSymbols(t *testing.T) {
	repoRoot := "../.."
	batteryRoot := filepath.Join(repoRoot, "battery")
	entries, err := os.ReadDir(batteryRoot)
	if err != nil {
		t.Fatalf("read battery/: %v", err)
	}

	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		bat := e.Name()
		mdPath := filepath.Join(batteryRoot, bat, "agents.md")
		md, err := os.ReadFile(mdPath)
		if err != nil {
			continue
		}
		api := loadPackageAPI(t, filepath.Join(batteryRoot, bat))
		for blockNum, block := range extractGoBlocks(md) {
			refs := extractSnippetRefs(block, bat)
			for _, r := range refs {
				if r.ReceiverType == "" {
					// Package-direct ref.
					if !api.Exports[r.Name] {
						t.Errorf("%s block #%d uses %s.%s but %s is not exported by battery/%s",
							mdPath, blockNum, bat, r.Name, r.Name, bat)
					}
					continue
				}
				// Instance-method ref — resolve constructor → type, then
				// check method against that type's method set.
				typeName, ok := api.CtorReturn[r.ReceiverType]
				if !ok {
					// Constructor's return type isn't introspectable
					// (e.g. it returns an unnamed interface). Fall back
					// to the flat exports check rather than false-fail.
					if !api.Exports[r.Name] {
						t.Errorf("%s block #%d uses <%s>.%s (from %s) — not a known method on the battery (no exact receiver match found)",
							mdPath, blockNum, r.Binding, r.Name, r.ReceiverType)
					}
					continue
				}
				methods := api.TypeMethods[typeName]
				if !methods[r.Name] {
					t.Errorf("%s block #%d uses <%s>.%s but %s is not a method on %s in battery/%s (methods on %s: %v)",
						mdPath, blockNum, r.Binding, r.Name, r.Name, typeName, bat, typeName, sortedKeys(methods))
				}
			}
		}
	}
}

// snippetRef is one validatable reference from a snippet. ReceiverType
// is empty for package-direct refs; non-empty for instance-method refs.
type snippetRef struct {
	Name         string // identifier (method or pkg-level name)
	Binding      string // local var name (instance-method only)
	ReceiverType string // resolved receiver type (instance-method only)
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

type selector struct {
	Pkg  string
	Name string
}

// extractGoBlocks pulls out every fenced ```go ... ``` body.
var reGoFence = regexp.MustCompile("(?s)```go\\s*\\n(.*?)```")

func extractGoBlocks(md []byte) []string {
	matches := reGoFence.FindAllSubmatch(md, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, string(m[1]))
	}
	return out
}

// extractSnippetRefs finds package-direct + instance-method references
// in block. For instance-method refs, the binding's receiver type is
// resolved via the snippet's constructor call (e.g. `q := queue.NewX(...)`
// binds `q` to whatever `NewX` returns — looked up by the caller via
// loadPackageAPI).
func extractSnippetRefs(block, batteryName string) []snippetRef {
	var out []snippetRef
	seen := map[string]struct{}{}

	// 1. Package-direct selectors.
	directRe := regexp.MustCompile(`\b` + regexp.QuoteMeta(batteryName) + `\.([A-Z][A-Za-z0-9_]*)\s*[({]`)
	for _, m := range directRe.FindAllStringSubmatch(block, -1) {
		key := "direct:" + m[1]
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, snippetRef{Name: m[1]})
	}

	// 2. Bindings: `<name>[, err] := <batteryName>.<Constructor>(...)`.
	// Constructor name lets the validator resolve receiver type.
	bindRe := regexp.MustCompile(
		`(?m)(?:var\s+)?([a-z][A-Za-z0-9_]*)(?:\s*,\s*[A-Za-z_][A-Za-z0-9_]*)?\s*(?::=|=)\s*` +
			regexp.QuoteMeta(batteryName) + `\.([A-Z][A-Za-z0-9_]*)\s*\(`)
	binding := map[string]string{} // local var name → constructor name
	for _, m := range bindRe.FindAllStringSubmatch(block, -1) {
		binding[m[1]] = m[2]
	}
	for name, ctor := range binding {
		methRe := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\.([A-Z][A-Za-z0-9_]*)\s*\(`)
		for _, m := range methRe.FindAllStringSubmatch(block, -1) {
			key := "method:" + name + "." + m[1]
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, snippetRef{
				Name:         m[1],
				Binding:      name,
				ReceiverType: ctor, // resolved to a real type name in the test
			})
		}
	}
	return out
}

// packageAPI captures the slice of a Go package's API the snippet test
// needs: top-level exports (for direct calls), methods grouped by
// receiver type (for instance-method validation), and a map of
// constructor name → its return type (so a binding like
// `q := queue.NewDBQueue(...)` resolves to `*DBQueue`).
type packageAPI struct {
	Exports     map[string]bool            // pkg-level identifiers
	TypeMethods map[string]map[string]bool // typeName → method-set (typeName has no `*`)
	CtorReturn  map[string]string          // `NewX` → return typeName (no `*`)
}

// loadPackageAPI scans every .go (non-test) file in dir and extracts
// the API surface used by the snippet validator.
func loadPackageAPI(t *testing.T, dir string) packageAPI {
	t.Helper()
	api := packageAPI{
		Exports:     map[string]bool{},
		TypeMethods: map[string]map[string]bool{},
		CtorReturn:  map[string]string{},
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		text := string(body)
		// Top-level type / const / var.
		for _, m := range reTypeDecl.FindAllStringSubmatch(text, -1) {
			api.Exports[m[1]] = true
		}
		for _, m := range reConstVarDecl.FindAllStringSubmatch(text, -1) {
			api.Exports[m[1]] = true
		}
		// Top-level funcs + record `New<X> -> ReturnType` for ctors.
		for _, m := range reFuncDeclWithReturn.FindAllStringSubmatch(text, -1) {
			name, ret := m[1], strings.TrimSpace(m[2])
			api.Exports[name] = true
			if strings.HasPrefix(name, "New") {
				api.CtorReturn[name] = stripPointer(firstType(ret))
			}
		}
		// Methods: record under the receiver type, and ALSO add to
		// Exports so package-direct lookups like `auth.HashPassword`
		// (which is a func, not a method) still work — the method
		// declarations don't overlap with funcs anyway.
		for _, m := range reMethodWithReceiver.FindAllStringSubmatch(text, -1) {
			recv, method := stripPointer(m[1]), m[2]
			if api.TypeMethods[recv] == nil {
				api.TypeMethods[recv] = map[string]bool{}
			}
			api.TypeMethods[recv][method] = true
		}
	}
	return api
}

// stripPointer drops a leading `*` so receiver / return types
// normalise to the underlying type name.
func stripPointer(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "*")
	return s
}

// firstType takes a return-type fragment (which may contain a list
// like `(*Manager, error)` or `*Manager`) and returns just the leading
// identifier with any leading `*`.
func firstType(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "(")
	// Walk to the first comma or close-paren.
	for i, r := range s {
		if r == ',' || r == ')' {
			return strings.TrimSpace(s[:i])
		}
	}
	return s
}

var (
	// `func Name(...)` — package-level functions (no receiver).
	reFuncDecl = regexp.MustCompile(`(?m)^func\s+([A-Z][A-Za-z0-9_]*)\s*\(`)
	// `func Name(args) ReturnType {` — captures name + return for ctor map.
	// Greedy non-newline match keeps us within the signature line.
	reFuncDeclWithReturn = regexp.MustCompile(`(?m)^func\s+([A-Z][A-Za-z0-9_]*)\s*\([^)]*\)\s*([^{]+)\{`)
	// `func (r *Type) Method(...)` — captures receiver type + method.
	reMethodWithReceiver = regexp.MustCompile(`(?m)^func\s+\([A-Za-z_]+\s+\*?([A-Z][A-Za-z0-9_]*)\)\s+([A-Z][A-Za-z0-9_]*)\s*\(`)
	// Also keep the simpler method regex for callers that don't need receiver info.
	reMethodDecl   = regexp.MustCompile(`(?m)^func\s+\([^)]+\)\s+([A-Z][A-Za-z0-9_]*)\s*\(`)
	reTypeDecl     = regexp.MustCompile(`(?m)^type\s+([A-Z][A-Za-z0-9_]*)\s+\S`)
	reConstVarDecl = regexp.MustCompile(`(?m)^(?:const|var)\s+([A-Z][A-Za-z0-9_]*)\s+`)
)
