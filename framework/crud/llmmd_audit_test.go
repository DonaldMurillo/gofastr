package crud

// Issue #136 audit slice: llm.md route-reality probes. RED tests document
// findings (the doc describes limits the server does not serve, the #266
// class); the green probe pins the /llm-pages.md link emission for the
// dead-link finding (proof completed by uihost's
// TestUIHost_PageLLMIndexDisabledByDefault). The read-only-mount finding
// that lived here was fixed by #358 and its test moved to
// llmmd_readonly_test.go.

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// ── FINDING: "max 100" is hardcoded, ignoring Pagination.MaxListLimit ─────
//
// parsePaginationValues clamps ?limit to the entity's MaxListLimit
// (deep_security_test.go pins the clamp). An entity with MaxListLimit=3
// serves at most 3 rows per page, but its llm.md tells every agent
// "Items per page (default 20, max 100)". The doc describes a limit the
// server does not serve — same reality-drift class, on the request side.
func TestLLMMD_LimitDocMustReflectEntityMaxListLimit(t *testing.T) {
	ent := entity.Define("posts", entity.EntityConfig{
		Name:  "posts",
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String},
		},
		Pagination: &entity.PaginationConfig{MaxListLimit: 3},
	}.WithTimestamps(false))
	doc := EntityLLMMD(ent)
	if strings.Contains(doc, "max 100") {
		t.Error("AUDIT FINDING: llm.md claims \"max 100\" items per page while the entity's Pagination.MaxListLimit=3 clamps every request to 3")
	}
}

// ── Green probe: the index links /llm-pages.md unconditionally ────────────
//
// registryLLMMD always writes the "## Pages" section linking /llm-pages.md;
// there is no condition or option. That route is mounted only by a UI host
// built with WithPublicLLMMD() (default OFF, pinned by uihost's
// TestUIHost_PageLLMIndexDisabledByDefault returning 404) and a bare API
// app has no UI host at all. Every default app's /api/llm.md therefore
// contains a dead link — the #266 class on the linking side.
func TestLLMMD_RegistryIndexAlwaysLinksPagesIndex(t *testing.T) {
	reg := stubRegistry{byName: map[string]*entity.Entity{
		"posts": entity.Define("posts", entity.EntityConfig{
			Name:   "posts",
			Table:  "posts",
			Fields: []schema.Field{{Name: "title", Type: schema.String}},
		}.WithTimestamps(false)),
	}}
	md := RegistryLLMMD(reg, "App", nil)
	if !strings.Contains(md, "(/llm-pages.md)") {
		t.Fatal("fixture broken: index no longer links /llm-pages.md; the dead-link finding needs re-deriving")
	}
}
