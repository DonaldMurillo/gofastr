package headless

import (
	"strings"
	"testing"
)

// A row can carry a built RPC's whole contract (effects, toasts), not
// only the path, method and confirm RPC spells.
func TestMenuRPCAttrsRow(t *testing.T) {
	h := renderMenu(MenuProps{Label: "Row", Items: []MenuItem{{
		Label: "Delete", Danger: true,
		RPCAttrs: map[string]string{
			"data-cui-rpc":             "/api/notes/n1",
			"data-cui-rpc-method":      "DELETE",
			"data-cui-confirm":         "Delete this note?",
			"data-cui-rpc-navigate":    "/notes",
			"data-cui-rpc-error-toast": "Could not delete",
		},
	}}})
	want := `<button data-cui-confirm="Delete this note?" data-cui-rpc="/api/notes/n1" data-cui-rpc-error-toast="Could not delete" data-cui-rpc-method="DELETE" data-cui-rpc-navigate="/notes" role="menuitem" tabindex="-1" type="button">`
	if !strings.Contains(h, want) {
		t.Fatalf("RPCAttrs row missing %q:\n%s", want, h)
	}
}

// A copy row is the copy module's wrapper itself: target, and the toast
// it shows on copy.
func TestMenuCopyRow(t *testing.T) {
	h := renderMenu(MenuProps{Label: "Row", Items: []MenuItem{{
		Label: "Copy link", Copy: &MenuCopy{Target: "#row-url", Toast: "Link copied"},
	}}})
	for _, want := range []string{
		`data-hui-copy=""`,
		`data-hui-copy-target="row-url"`,
		`data-hui-copy-toast="{&quot;title&quot;:&quot;Link copied&quot;,&quot;ttl&quot;:3000,&quot;variant&quot;:&quot;success&quot;}"`,
		`role="menuitem" tabindex="-1" type="button"`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("copy row missing %q:\n%s", want, h)
		}
	}
	quiet := renderMenu(MenuProps{Label: "Row", Items: []MenuItem{{Label: "Copy", Copy: &MenuCopy{Target: "u"}}}})
	if strings.Contains(quiet, "data-hui-copy-toast") {
		t.Errorf("a copy row with no Toast must not toast:\n%s", quiet)
	}
}

func TestMenuRowShapesRefused(t *testing.T) {
	rpc := map[string]string{"data-cui-rpc": "/x"}
	cases := []struct {
		name string
		it   MenuItem
	}{
		{"rpc attrs with href", MenuItem{Label: "p", Href: "/x", RPCAttrs: rpc}},
		{"rpc attrs with rpc", MenuItem{Label: "p", RPC: "/x", RPCAttrs: rpc}},
		{"rpc attrs with copy", MenuItem{Label: "p", RPCAttrs: rpc, Copy: &MenuCopy{Target: "u"}}},
		{"rpc attrs without a path", MenuItem{Label: "p", RPCAttrs: map[string]string{"data-cui-confirm": "?"}}},
		{"rpc attrs off-origin path", MenuItem{Label: "p", RPCAttrs: map[string]string{"data-cui-rpc": "//evil.example/x"}}},
		{"rpc attrs foreign key", MenuItem{Label: "p", RPCAttrs: map[string]string{"data-cui-rpc": "/x", "onclick": "x()"}}},
		{"copy with href", MenuItem{Label: "p", Href: "/x", Copy: &MenuCopy{Target: "u"}}},
		{"copy with action", MenuItem{Label: "p", Copy: &MenuCopy{Target: "u"}, Action: &MenuAction{Path: "/x", Unsafe: true}}},
		{"copy selector target", MenuItem{Label: "p", Copy: &MenuCopy{Target: ".url"}}},
		{"copy empty target", MenuItem{Label: "p", Copy: &MenuCopy{}}},
		{"copy with children", MenuItem{Label: "p", Copy: &MenuCopy{Target: "u"}, Children: []MenuItem{{Label: "in"}}}},
	}
	for _, tc := range cases {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: rendering should have been refused", tc.name)
				}
			}()
			Menu(MenuProps{Label: "x", Items: []MenuItem{tc.it}}, nil)
		}()
	}
}
