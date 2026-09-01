package crud

// Issue #136 audit slice: llm.md route-reality probes. The limits finding
// documented here (the doc described caps the server did not serve, the
// #266 class) was fixed with #358 and its test is now the standing pin;
// the green probe below pins the /llm-pages.md link emission for the
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

// ── Pin: the limit row reports the cap the List route enforces ────────────
//
// parsePaginationValues clamps ?limit through listLimitCap
// (deep_security_test.go pins the clamp), so the doc has to print that
// same cap or it advertises a limit the server refuses — the #266
// reality-drift class, on the request side. Found as a hardcoded
// "max 100" against MaxListLimit=3. The assertion pins the exact row:
// the first draft only rejected `max 100`, which passes on ANY other
// wrong cap (0, 50, 99) — an assertion that rules out one wrong answer
// instead of the right one.
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
	const want = "Items per page (default 20, max 3)"
	if !strings.Contains(doc, want) {
		t.Errorf("llm.md limit row does not report the entity's cap; want %q in:\n%s", want, doc)
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
