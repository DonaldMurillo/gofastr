package disclosure

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestRequiresTitle(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Disclosure without Title should panic")
		}
	}()
	Render(Config{}, render.Text("body"))
}

func TestRendersDetailsSummary(t *testing.T) {
	h := string(Render(Config{Title: "FAQ"}, render.Text("Body content")))
	if !strings.Contains(h, "<details") {
		t.Errorf("expected <details> wrapper:\n%s", h)
	}
	if !strings.Contains(h, "<summary") {
		t.Errorf("expected <summary>:\n%s", h)
	}
	if !strings.Contains(h, ">FAQ<") {
		t.Errorf("Title should render inside <summary>:\n%s", h)
	}
	if !strings.Contains(h, "Body content") {
		t.Errorf("Body children should render:\n%s", h)
	}
}

func TestOpenAttrEmitted(t *testing.T) {
	closed := string(Render(Config{Title: "x"}))
	if strings.Contains(closed, " open") {
		t.Errorf("default should not emit open:\n%s", closed)
	}
	open := string(Render(Config{Title: "x", Open: true}))
	if !strings.Contains(open, " open") {
		t.Errorf("Open=true should emit open attr:\n%s", open)
	}
}

func TestDataFuiCompEmitted(t *testing.T) {
	h := string(Render(Config{Title: "x"}))
	if !strings.Contains(h, `data-fui-comp="ui-disclosure"`) {
		t.Errorf("disclosure should emit data-fui-comp marker:\n%s", h)
	}
}
