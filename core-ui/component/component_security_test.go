package component

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// panicComponent panics with a caller-supplied message during Render.
type panicComponent struct{ msg string }

func (p panicComponent) Render() render.HTML { panic(p.msg) }

// TestSafeRenderEscapesPanicMessage asserts the fallback error UI
// HTML-escapes attacker-influenced panic text so it cannot inject markup.
func TestSafeRenderEscapesPanicMessage(t *testing.T) {
	cases := []struct {
		name   string
		msg    string
		raw    string // substring that must NOT appear verbatim (would be live markup)
		escErr string // escaped form that proves the message survived as text
	}{
		{
			name:   "img onerror",
			msg:    `<img src=x onerror=alert(1)>`,
			raw:    `<img src=x onerror=alert(1)>`,
			escErr: `&lt;img src=x onerror=alert(1)&gt;`,
		},
		{
			name:   "script tag",
			msg:    `</strong><script>alert(1)</script>`,
			raw:    `<script>alert(1)</script>`,
			escErr: `&lt;script&gt;`,
		},
		{
			name:   "attribute breakout",
			msg:    `"><svg/onload=alert(1)>`,
			raw:    `<svg/onload=alert(1)>`,
			escErr: `&lt;svg/onload=alert(1)&gt;`,
		},
		{
			name:   "benign message renders",
			msg:    `oh no`,
			raw:    "",
			escErr: `oh no`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			html, err := SafeRender(panicComponent{msg: tc.msg})
			if err == nil {
				t.Fatal("expected an error from the panic")
			}
			out := string(html)
			if tc.raw != "" && strings.Contains(out, tc.raw) {
				t.Errorf("fallback UI contains live markup %q:\n%s", tc.raw, out)
			}
			if !strings.Contains(out, tc.escErr) {
				t.Errorf("fallback UI missing escaped message %q:\n%s", tc.escErr, out)
			}
		})
	}
}
