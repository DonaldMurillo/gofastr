package crud

// Issue #136 audit slice: llm.md route-reality probes. The limits finding
// documented here (the doc described caps the server did not serve, the
// #266 class) was fixed with #358 and its test is now the standing pin;
// the dead /llm-pages.md link is fixed and pinned below, with the
// route-table side in framework's TestLLMIndexPagesLinkMatchesRoutes.
// The read-only-mount finding
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
	// The default is clamped too: a list sent no ?limit answers 3 rows.
	const want = "Items per page (default 3, max 3)"
	if !strings.Contains(doc, want) {
		t.Errorf("llm.md limit row does not report the entity's cap; want %q in:\n%s", want, doc)
	}
}

// The index links the page index only at the path the app says it serves:
// /llm-pages.md is mounted only by a UI host built with WithPublicLLMMD, so
// an empty path drops the Pages section rather than link a 404.
func TestRegistryIndexPagesLinkFollowsPath(t *testing.T) {
	reg := stubRegistry{byName: map[string]*entity.Entity{
		"posts": entity.Define("posts", entity.EntityConfig{
			Name:   "posts",
			Table:  "posts",
			Fields: []schema.Field{{Name: "title", Type: schema.String}},
		}.WithTimestamps(false)),
	}}
	if md := RegistryLLMMD(reg, "App", nil, ""); strings.Contains(md, "llm-pages.md") || strings.Contains(md, "## Pages") {
		t.Errorf("no page index served, but the index links one:\n%s", md)
	}
	if md := RegistryLLMMD(reg, "App", nil, "/site/llm-pages.md"); !strings.Contains(md, "[/site/llm-pages.md](/site/llm-pages.md)") {
		t.Errorf("index does not link the served page index:\n%s", md)
	}
}
