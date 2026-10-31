package print

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// PathPrefix "/" trimmed to "", so the routes mounted at the root while
// ReservedEmbedPrefixes named /print, and an embed grant reached the
// headless-Chrome PDF route the reservation keeps it from (2026-10-27
// B48). A prefix that is not a path below the root is refused at mount,
// so the reservation always names what is mounted.
func TestPrintRefusesRootPrefix(t *testing.T) {
	for _, prefix := range []string{"/", "//", "print"} {
		b := New(Config{PathPrefix: prefix})
		b.Document(Document{Name: "invoice", Path: "/invoice/{id}", Build: docBuild("<p>x</p>")})
		err := b.RegisterRoutes(router.New())
		if err == nil || !strings.Contains(err.Error(), "PathPrefix") {
			t.Fatalf("PathPrefix %q: RegisterRoutes = %v, want a PathPrefix refusal", prefix, err)
		}
	}
	b := New(Config{PathPrefix: "/docs/print/"})
	if got := b.ReservedEmbedPrefixes(); len(got) != 1 || got[0] != "/docs/print" {
		t.Fatalf("ReservedEmbedPrefixes = %v, want [/docs/print]", got)
	}
}
