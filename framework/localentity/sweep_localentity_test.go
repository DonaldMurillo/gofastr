package localentity

import (
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/schema"
)

func TestPatternQuantifierForms(t *testing.T) {
	for _, p := range []string{`a{2}`, `a{2,}`, `a{2,5}`} {
		if err := checkPattern(p); err != nil {
			t.Errorf("checkPattern(%q) = %v, want accepted", p, err)
		}
	}
	if checkPattern(`a{,5}`) == nil {
		t.Error("checkPattern accepted a { that is not a quantifier")
	}
}

var sweepFail = Define(testDB, "sweepfail", []schema.Field{{Name: "n", Type: schema.String}}, WithMessages(Messages{Failed: "Not saved"}))

func TestListCarriesFailedMessage(t *testing.T) {
	l := sweepFail.List(ListConfig{})
	h := l.Render(l.Row(func(Row) render.HTML { return "<div></div>" }), l.Empty("<p></p>"))
	if got := attr(t, h, "data-fui-local-fail"); got != "Not saved" {
		t.Fatalf("fail attribute = %q, want the Failed message", got)
	}
}

func TestBehavioursAndCarrierStyle(t *testing.T) {
	form, _ := registry.LookupBehavior("localentity-form")
	if !slices.Equal(form.Requires, []string{"localdb", "formerrors"}) {
		t.Errorf("localentity-form requires %v, want [localdb formerrors]", form.Requires)
	}
	list, _ := registry.LookupBehavior("localentity")
	if !slices.Equal(list.Requires, []string{"localdb"}) {
		t.Errorf("localentity requires %v, want [localdb]", list.Requires)
	}
	if css := carrierStyle.Entry().StyleFn(style.Theme{}); !strings.Contains(css, "display:contents") {
		t.Errorf("carrier style %q does not lay the carriers out as absent", css)
	}
}
