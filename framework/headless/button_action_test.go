package headless

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
)

// A button that fires a request says so through one named seam, and
// nothing else can put a request on it.
//
// The host framework's wrappers splice their attributes into the
// rendered string at the first '>'. That works, and it also means a
// request can be attached to any markup from outside, invisibly to the
// component and to whoever reads its props. Here the request is a
// prop: reviewable in one place, refused on a link, and impossible to
// smuggle in as decoration.
func TestButtonCarriesARequestOnlyThroughAction(t *testing.T) {
	req := html.Attrs{
		"data-fui-rpc":        "/apps/api/restart",
		"data-fui-rpc-method": "POST",
		"data-fui-rpc-signal": "apps",
		"data-fui-confirm":    "Restart api?",
	}
	got := Button(ButtonProps{Label: "Restart", Action: req}, nil)
	for k, v := range req {
		has(t, got, k+`="`+v+`"`, "the action's "+k+" did not land on the button")
	}

	// The same attributes through ExtraAttrs are dropped: a test id
	// must never be able to turn into a request.
	smuggled := Button(ButtonProps{Label: "Restart", ExtraAttrs: req}, nil)
	for k := range req {
		hasNot(t, smuggled, k, "a request arrived through ExtraAttrs, which is for decoration")
	}
}

func TestButtonActionAcceptsOnlyRequestAttributes(t *testing.T) {
	for _, k := range []string{"data-fui-signal", "data-fui-poll", "data-fui-comp", "data-fui-optimistic-endpoint", "onclick", "data-hui-copy"} {
		func() {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("Action carrying %q was accepted; only a request belongs in this seam", k)
				} else if !strings.Contains(r.(string), k) {
					t.Errorf("the refusal for %q does not name it: %v", k, r)
				}
			}()
			Button(ButtonProps{Label: "x", Action: html.Attrs{k: "y", "data-fui-rpc": "/x"}}, nil)
		}()
	}
}

func TestButtonRefusesAnActionOnALink(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("a link with a request on it was rendered; a link navigates and a button acts")
		}
	}()
	Button(ButtonProps{Label: "Go", Href: "/apps", Action: html.Attrs{"data-fui-rpc": "/x"}}, nil)
}

// An empty Action is the common case and must cost nothing: no
// attributes, no panic, the same markup as before the seam existed.
func TestButtonWithoutAnActionIsUnchanged(t *testing.T) {
	plain := Button(ButtonProps{Label: "Save"}, nil)
	withNil := Button(ButtonProps{Label: "Save", Action: nil}, nil)
	withEmpty := Button(ButtonProps{Label: "Save", Action: html.Attrs{}}, nil)
	if plain != withNil || plain != withEmpty {
		t.Errorf("an absent action changed the markup:\n%s\n%s\n%s", plain, withNil, withEmpty)
	}
	hasNot(t, plain, "data-fui", "a button with no action carries framework attributes")
}

// The wiring keys core-ui/interactive can splice onto a clickable are
// vocabulary too: what a click opens, closes, toasts, writes into the
// URL, deep-links, or prefetches travels the same seam as a request,
// each checked for what it deserves.
func TestButtonActionAdmitsTheWiringKeys(t *testing.T) {
	for k, v := range map[string]string{
		"data-fui-open":              "user-edit",
		"data-hui-pane-open-control": "secondary",
		"data-hui-pane-key":          "ticket-42",
		"data-fui-intercept-close":   "",
		"data-fui-toast":             `{"variant":"success","title":"Saved"}`,
		"data-hui-pane-close":        "",
		"data-fui-push-state":        "/apps/42",
		"data-fui-deeplink":          "user_id=42",
		"data-fui-prefetch":          "tabs fileupload",
		"data-fui-rpc-open":          "result-modal",
		"data-fui-rpc-refresh":       "my-widget",
		"data-fui-rpc-close":         "true",
		"data-fui-rpc-reset":         "true",
		"data-fui-rpc-navigate":      "/apps/42",
		"data-fui-rpc-body":          `{"a":1}`,
	} {
		got := Button(ButtonProps{Label: "Act", Action: html.Attrs{"data-fui-rpc": "/x", k: v}}, nil)
		has(t, got, k, "the wiring key "+k+" did not land on the button")
	}
	// after-text and after-disable and scroll-to ride beside an rpc;
	// after-disable is presence-valued, so it lands bare.
	got := Button(ButtonProps{Label: "Save", Action: html.Attrs{
		"data-fui-rpc": "/x", "data-fui-rpc-after-text": "Saved",
		"data-fui-rpc-after-disable": "", "data-fui-rpc-scroll-to": "#item",
	}}, nil)
	has(t, got, `data-fui-rpc-after-text="Saved"`, "after-text did not land")
	has(t, got, "data-fui-rpc-after-disable", "after-disable did not land")
	has(t, got, `data-fui-rpc-scroll-to="#item"`, "scroll-to did not land")
}

// A wiring key that would fire something — none of them do — or a
// request on an anchor: the link carries only what may ride it. The
// four link-legal keys stay, because they say where a click goes
// without firing anything.
func TestButtonLinkCarriesOnlyLinkLegalActions(t *testing.T) {
	got := Button(ButtonProps{Label: "Docs", Href: "/docs", Action: html.Attrs{
		"data-fui-push-state": "/docs", "data-fui-prefetch": "menu",
		"data-fui-open": "help", "data-fui-deeplink": "topic=ssh",
	}}, nil)
	for _, want := range []string{
		`data-fui-push-state="/docs"`, `data-fui-prefetch="menu"`,
		`data-fui-open="help"`, `data-fui-deeplink="topic=ssh"`,
	} {
		has(t, got, want, "a link-legal wiring key was refused on the anchor")
	}
	for _, k := range []string{
		"data-fui-rpc", "data-fui-rpc-close", "data-fui-rpc-navigate",
		"data-fui-rpc-refresh", "data-fui-confirm", "data-fui-signal-inc",
		"data-fui-pane-open", "data-fui-pane-key", "data-fui-toast",
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("an anchor carried %q — a link navigates, a button acts", k)
				}
			}()
			Button(ButtonProps{Label: "Go", Href: "/apps", Action: html.Attrs{k: "secondary", "data-fui-open": "w"}}, nil)
		}()
	}
}

// Each check exists because a value that fails it is a button that
// looks wired and does nothing: an empty name, a foreign origin, a
// method or pane the runtime never answers to, a payload that fails
// to parse at click time.
func TestButtonActionChecksItsValues(t *testing.T) {
	cases := []html.Attrs{
		{"data-fui-rpc": ""},
		{"data-fui-rpc": "//evil/x"},
		{"data-fui-rpc-refresh": ""},
		{"data-hui-pane-key": ""},
		{"data-fui-open": ""},
		{"data-fui-deeplink": ""},
		{"data-fui-toast": ""},
		{"data-fui-toast": `{"variant":`},
		{"data-fui-push-state": ""},
		{"data-fui-push-state": "https://evil.example/x"},
		{"data-fui-push-state": "//evil/x"},
		{"data-fui-rpc-navigate": "https://evil.example/x"},
		{"data-fui-rpc-navigate": ""},
		{"data-fui-rpc-method": "TRACE"},
		{"data-fui-rpc-body": `{not json`},
		{"data-fui-rpc-open": ""},
		{"data-fui-rpc-after-text": ""},
		{"data-fui-rpc-scroll-to": ""},
		{"data-fui-confirm": ""},
		{"data-hui-pane-open-control": "primary"},
		{"data-hui-pane-close": "left"},
		{"data-fui-prefetch": "../../../evil"},
		{"data-fui-prefetch": "name/with/slashes"},
	}
	for _, a := range cases {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Action %v was accepted unchecked", a)
				}
			}()
			Button(ButtonProps{Label: "x", Action: a}, nil)
		}()
	}
}

// Attribute names fold in HTML: a spelling in any case is the same
// attribute to the runtime, so it is checked and stored under one
// canonical name — and one key under two spellings is a duplicate the
// caller cannot see in the rendered tag.
func TestButtonActionFoldsAndRefusesDuplicateSpellings(t *testing.T) {
	got := Button(ButtonProps{Label: "Open", Action: html.Attrs{"DATA-FUI-OPEN": "help"}}, nil)
	has(t, got, `data-fui-open="help"`, "an upper-case spelling did not land under its folded name")
	func() {
		defer func() {
			if recover() == nil {
				t.Error("one key under two spellings was accepted")
			}
		}()
		Button(ButtonProps{Label: "x", Action: html.Attrs{"data-fui-open": "a", "Data-Fui-Open": "b"}}, nil)
	}()
	func() {
		defer func() {
			if recover() == nil {
				t.Error("an upper-case rpc spelling dodged the same-origin check")
			}
		}()
		Button(ButtonProps{Label: "x", Action: html.Attrs{"DATA-FUI-RPC": "//evil/x"}}, nil)
	}()
}

// A class a caller appends through Parts lands after the class map's
// own, never instead of it, and the shared map is not mutated.
func TestButtonPartsAppendTheRootClass(t *testing.T) {
	classes := Classes{PartRoot: "btn", PartIcon: "btn__icon"}
	got := Button(ButtonProps{Label: "Save", Parts: Parts{Attrs: PartAttrs{
		PartRoot: {"class": "mine"},
	}}}, classes)
	has(t, got, `class="btn mine"`, "the caller's class must append, not replace")
	if classes[PartRoot] != "btn" {
		t.Error("the class map itself was mutated")
	}
}
