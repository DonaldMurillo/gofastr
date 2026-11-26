package uiaction

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestButtonRenderTakesNilAttrs(t *testing.T) {
	b := Button{Endpoint: "/x", State: "idle"}
	got := b.Render(nil, func(tag, _ string, attrs map[string]string, children ...render.HTML) render.HTML {
		return render.Tag(tag, attrs, children...)
	})
	if !strings.Contains(string(got), `data-hui-action-endpoint="/x"`) {
		t.Fatalf("Render(nil) = %s", got)
	}
}
