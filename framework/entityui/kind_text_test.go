package entityui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// A text slot (a row link's label, a card's title) takes a kind's Text,
// else the field-type text, and never its Cell HTML: the Cell's markup
// cast to a string and escaped again printed as tags to the reader.
func TestKindTextInTextSlots(t *testing.T) {
	shout := func(cc CellContext) render.HTML { return render.HTML("<strong>" + cell(cc.Value) + "</strong>") }
	cases := []struct {
		name  string
		kind  Kind
		title string // the title field the kind draws
		want  string
	}{
		{"kind with Text", Kind{Cell: shout, Text: func(cc CellContext) string { return "Deal " + cell(cc.Value) }}, "name", ">Deal alpha<"},
		{"kind without Text", Kind{Cell: shout}, "name", ">alpha<"},
		{"built-in money", Kind{}, "amount", ">$1,234.50<"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := dealsConfig()
			cfg.Display.TitleFields = []string{c.title}
			ext := Extensions{}
			if c.kind.Cell != nil {
				cfg.Display.Fields["name"] = entity.FieldDisplay{Input: "shout"}
				ext.Kinds = map[string]Kind{"shout": c.kind}
			}
			x := newTestUIExt(t, map[string]entity.EntityConfig{"deals": cfg},
				map[string][]map[string]any{"deals": {{"id": "d1", "name": "alpha", "amount": "1234.5", "fee": "-5"}}},
				ext, withAPI(map[string]string{"deals": "/api/deals"}))
			ctx := x.userCtx("/deals", "", "u1")
			for _, as := range []string{"table", "cards"} {
				body := string(x.ui.List("deals").Key(as).Base("/deals").Columns(c.title).As(as).RenderCtx(ctx))
				if strings.Contains(body, "&lt;") {
					t.Errorf("%s: a text slot printed escaped markup:\n%s", as, body)
				}
				if !strings.Contains(body, c.want) {
					t.Errorf("%s: no %q in:\n%s", as, c.want, body)
				}
			}
		})
	}
}

// A row whose link column is empty still draws: ui.Link refuses empty
// text, so the label falls back rather than panicking the list.
func TestEmptyLinkLabelDraws(t *testing.T) {
	for _, title := range []string{"name", "amount"} {
		t.Run(title, func(t *testing.T) {
			cfg := dealsConfig()
			cfg.Display.TitleFields = []string{title}
			x := newTestUIExt(t, map[string]entity.EntityConfig{"deals": cfg},
				map[string][]map[string]any{"deals": {{"id": "d1", "name": "", "amount": "", "fee": "-5"}}},
				Extensions{}, withAPI(map[string]string{"deals": "/api/deals"}))
			ctx := x.userCtx("/deals", "", "u1")
			body := string(x.ui.List("deals").Base("/deals").Columns(title, "fee").RenderCtx(ctx))
			if !strings.Contains(body, `href="/deals/d1"`) {
				t.Errorf("the row with an empty %s drew no record link:\n%s", title, body)
			}
		})
	}
}
