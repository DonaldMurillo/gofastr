package cache

//gofastr:test-reads /framework/app.go

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// The cache's credential list must match the one framework/app.go uses to
// namespace callers (credentialFingerprint), which framework's
// TestCredentialCanonsAgree holds against the broker and MCP lists. The
// cache missed X-API-Key while the others named it (round 7 R7-059); this
// reads the framework list from source so the next credential header
// fails here instead of being cached.
func TestCacheCredentialCanon(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "framework", "app.go"))
	if err != nil {
		t.Fatalf("read framework/app.go: %v", err)
	}
	body := string(src)
	i := strings.Index(body, "func credentialFingerprint")
	if i < 0 {
		t.Fatal("framework/app.go: credentialFingerprint not found; the canon moved")
	}
	m := regexp.MustCompile(`\[\]string\{([^}]*)\}`).FindStringSubmatch(body[i:])
	if m == nil {
		t.Fatal("framework/app.go: no []string literal in credentialFingerprint")
	}
	var want []string
	for _, f := range strings.Split(m[1], ",") {
		f = strings.TrimSpace(f)
		if f == "fembed.GrantHeader" {
			f = strconv.Quote("X-Gofastr-Embed")
		}
		s, err := strconv.Unquote(f)
		if err != nil {
			t.Fatalf("framework/app.go: unexpected list element %q", f)
		}
		want = append(want, s)
	}
	got := slices.Clone(credentialHeaders)
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("cache credentialHeaders = %v, framework credentialFingerprint = %v", got, want)
	}
}
