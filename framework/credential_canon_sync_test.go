package framework

// Three lists name the framework's credential headers, and they must agree.
//
//	app.go            credentialFingerprint  — which credentials namespace a caller
//	processmodule_broker.go  delegationCredentialHeaders — which the snapshot carries
//	crud/mcp.go       runToolRequest         — which the re-dispatch re-injects
//
// A header one list recognises and another drops is the #360 defect class:
// authority the caller legitimately holds that never reaches the
// re-dispatch. That shipped, and it shipped as a silent 401 rather than an
// error anyone could trace. The fix left the three lists cross-referenced by
// comment and nothing enforcing it, which is a promise rather than a
// guarantee — a fifth credential header means editing three places and
// noticing you had to.
//
// This reads the lists out of the source rather than restating them. A test
// that declared its own copy of the canon would be a FOURTH list to keep in
// sync, and would pass while the three real ones diverged.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// headerStringsNear returns the string literals of the first []string
// composite literal that appears after the named identifier in src.
func headerStringsNear(t *testing.T, path, anchor string) []string {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	idx := strings.Index(string(src), anchor)
	if idx < 0 {
		t.Fatalf("%s: anchor %q not found — the canon moved or was renamed, which is exactly the drift this test exists for", filepath.Base(path), anchor)
	}
	anchorPos := token.Pos(int(f.Pos()) + idx)

	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		cl, ok := n.(*ast.CompositeLit)
		if !ok || cl.Pos() < anchorPos || out != nil {
			return true
		}
		at, ok := cl.Type.(*ast.ArrayType)
		if !ok {
			return true
		}
		if id, ok := at.Elt.(*ast.Ident); !ok || id.Name != "string" {
			return true
		}
		for _, e := range cl.Elts {
			switch v := e.(type) {
			case *ast.BasicLit:
				if v.Kind == token.STRING {
					s, err := strconv.Unquote(v.Value)
					if err == nil {
						out = append(out, s)
					}
				}
			case *ast.SelectorExpr:
				// fembed.GrantHeader and friends: record the selector so a
				// list that swaps a constant for a literal still compares.
				out = append(out, v.Sel.Name)
			}
		}
		return true
	})
	if len(out) == 0 {
		t.Fatalf("%s: no []string literal found after %q", filepath.Base(path), anchor)
	}
	return out
}

func TestCredentialCanonsAgree(t *testing.T) {
	// Selector names normalise to the header they resolve to, so the three
	// spellings compare on meaning rather than syntax.
	norm := func(in []string) []string {
		out := make([]string, 0, len(in))
		for _, s := range in {
			if s == "GrantHeader" {
				s = "X-Gofastr-Embed"
			}
			out = append(out, s)
		}
		slices.Sort(out)
		return out
	}

	fingerprint := norm(headerStringsNear(t, "app.go", "func credentialFingerprint"))
	broker := norm(headerStringsNear(t, "processmodule_broker.go", "delegationCredentialHeaders ="))
	redispatch := norm(headerStringsNear(t, filepath.Join("crud", "mcp.go"), "func runToolRequest"))

	t.Logf("credentialFingerprint       %v", fingerprint)
	t.Logf("delegationCredentialHeaders %v", broker)
	t.Logf("runToolRequest              %v", redispatch)

	if !slices.Equal(fingerprint, broker) {
		t.Errorf("SECURITY: [#360] credentialFingerprint and delegationCredentialHeaders disagree:\n  app.go:    %v\n  broker.go: %v\nA header one recognises and the other drops is authority the caller holds that never reaches the delegation.", fingerprint, broker)
	}
	if !slices.Equal(fingerprint, redispatch) {
		t.Errorf("SECURITY: [#360] credentialFingerprint and runToolRequest's copy list disagree:\n  app.go:      %v\n  crud/mcp.go: %v\nThis is the half that made the original fix incomplete: the broker was corrected and the re-dispatch was not, so API-key callers stayed denied.", fingerprint, redispatch)
	}
}
