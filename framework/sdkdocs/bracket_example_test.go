package sdkdocs

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// The example snippets spell the filter key the way the server reads it:
// on an entity with amount beside amount_gte, ?amount_gte= reads two ways
// and answers 400, so the snippets send amount[gte] (escaped for curl,
// which reads a literal [..] as a glob). An entity with no such pair
// keeps the suffix spelling.
func TestExampleTabsSpellCollidingKey(t *testing.T) {
	s := &site{cfg: Config{Registry: testRegistry(), BasePath: "/docs/api"}}
	sc := &entityScreen{site: s}
	colliding := entity.Define("ledgers", entity.EntityConfig{
		Table: "ledgers",
		Fields: []schema.Field{
			{Name: "amount", Type: schema.Int},
			{Name: "amount_gte", Type: schema.Int},
		},
	})
	html := sc.exampleTabs(colliding).String()
	for _, want := range []string{"amount[gte]", "amount%5Bgte%5D=10"} {
		if !strings.Contains(html, want) {
			t.Errorf("colliding entity's examples lack %q:\n%s", want, html)
		}
	}
	if strings.Contains(html, "amount_gte=10") {
		t.Errorf("colliding entity's curl example sends the ambiguous bare key:\n%s", html)
	}
	plain := entity.Define("tallies", entity.EntityConfig{
		Table:  "tallies",
		Fields: []schema.Field{{Name: "amount", Type: schema.Int}},
	})
	if html := sc.exampleTabs(plain).String(); !strings.Contains(html, "amount_gte=10") {
		t.Errorf("plain entity's curl example changed:\n%s", html)
	}
}
