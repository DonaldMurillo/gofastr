package store

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestPublishWiresRPCSignal(t *testing.T) {
	resetForTest()
	sl := New("org").String("companyName", "Acme")
	btn := render.Tag("button", nil, render.Text("Rename"))
	html := string(interactive.OnClick(btn, sl.Publish(interactive.Post("/island/org/rename"))))

	if !strings.Contains(html, `data-fui-rpc="/island/org/rename"`) {
		t.Errorf("missing rpc path: %s", html)
	}
	if !strings.Contains(html, `data-fui-rpc-signal="org.companyName"`) {
		t.Errorf("Publish did not wire the qualified signal name: %s", html)
	}
}
