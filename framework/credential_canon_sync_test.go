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
// This reads the lists out of the source rather than restating them, so a
// divergence between the three is visible. It ALSO asserts the canon
// absolutely, because cross-comparison alone cannot see a coordinated
// omission — dropping a header from all three leaves them agreeing. The two
// checks catch different failures and the second needs the first to stay
// honest; see the comment above `want` below.

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

	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
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
	// The norm below maps the selector NAME "GrantHeader" to a literal.
	// That mapping is only honest while the constant's VALUE is that
	// literal — if fembed.GrantHeader changed, all three lists would keep
	// agreeing here while the runtime compared a different header. Bind
	// the name to the value it is normalised to.
	if fembed.GrantHeader != "X-Gofastr-Embed" {
		t.Fatalf("fembed.GrantHeader = %q; this test normalises the selector to %q — update the mapping AND the want list together",
			fembed.GrantHeader, "X-Gofastr-Embed")
	}

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

	// Cross-comparison alone catches DIVERGENCE — one list changed — but
	// not a coordinated omission: drop Cookie from all three and they still
	// agree with each other. So the canon is also asserted absolutely.
	//
	// I argued against this at first, on the grounds that a declared copy
	// is a fourth list that can drift. That objection is weaker than it
	// looks: the cross-checks above still run, so the drift I feared (this
	// test passing while the real lists disagree) cannot happen. And a
	// legitimate fifth credential header SHOULD fail here — a deliberate,
	// visible edit is exactly what you want when the security canon grows.
	want := []string{"Authorization", "Cookie", "X-API-Key", "X-Gofastr-Embed"}
	for _, c := range []struct {
		name string
		got  []string
	}{
		{"credentialFingerprint", fingerprint},
		{"delegationCredentialHeaders", broker},
		{"runToolRequest", redispatch},
	} {
		if !slices.Equal(c.got, want) {
			t.Errorf("SECURITY: [#360] %s is %v, want %v. If a credential header was deliberately added or removed, update this list in the same change — that edit is the point, not an obstacle.", c.name, c.got, want)
		}
	}

	if !slices.Equal(fingerprint, broker) {
		t.Errorf("SECURITY: [#360] credentialFingerprint and delegationCredentialHeaders disagree:\n  app.go:    %v\n  broker.go: %v\nA header one recognises and the other drops is authority the caller holds that never reaches the delegation.", fingerprint, broker)
	}
	if !slices.Equal(fingerprint, redispatch) {
		t.Errorf("SECURITY: [#360] credentialFingerprint and runToolRequest's copy list disagree:\n  app.go:      %v\n  crud/mcp.go: %v\nThis is the half that made the original fix incomplete: the broker was corrected and the re-dispatch was not, so API-key callers stayed denied.", fingerprint, redispatch)
	}
}
