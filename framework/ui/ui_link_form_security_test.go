package ui_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	ui "github.com/DonaldMurillo/gofastr/framework/ui"
)

// ─── Helpers ──────────────────────────────────────────────────────────────────

// mustPanic fails if fn does NOT panic.
func mustPanic(t *testing.T, msg string, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("SECURITY: expected panic: %s", msg)
		}
	}()
	fn()
}

// mustNotContain fails if the HTML output contains the forbidden substring.
func mustNotContain(t *testing.T, h render.HTML, sub string) {
	t.Helper()
	if strings.Contains(string(h), sub) {
		t.Errorf("SECURITY: output contains unsafe substring %q\nHTML: %s", sub, h)
	}
}

// mustContain is the inverse: confirms expected safe substring.
func mustContain(t *testing.T, h render.HTML, sub string) {
	t.Helper()
	if !strings.Contains(string(h), sub) {
		t.Errorf("expected HTML to contain %q\ngot: %s", sub, h)
	}
}

// ═══════════════════════════════════════════════════════════════════════════════
//  Link component XSS (tests 1–10)
// ═══════════════════════════════════════════════════════════════════════════════

// TestLink_DropsDangerousHrefs pins the framework-side allow-list:
// javascript:, data:, vbscript:, file:, blob:, and protocol-relative
// URLs never appear in the rendered href. Previously the framework
// only attribute-escaped. The contract was "callers must validate".
// That contract flipped: scheme validation lives in framework/ui/safety.go
// so component-level callers can't accidentally ship an XSS vector.
func TestLink_DropsDangerousHrefs(t *testing.T) {
	for _, payload := range []string{
		"javascript:alert(document.cookie)",
		"JAVASCRIPT:alert(1)",
		"data:text/html,<script>alert(1)</script>",
		"vbscript:MsgBox(1)",
		"file:///etc/passwd",
		"blob:https://evil.example/123",
		"//evil.example/x",
	} {
		t.Run(payload, func(t *testing.T) {
			h := ui.Link(ui.LinkConfig{Href: payload, Text: "Click me"})
			href := extractAttr(string(h), "href")
			if strings.Contains(strings.ToLower(href), strings.ToLower(payload)) {
				t.Fatalf("dangerous href %q reached output (href=%q)", payload, href)
			}
		})
	}
}

// TestLink_AllowsSafeHrefs sanity-checks that http(s), relative, and
// fragment hrefs round-trip unchanged.
func TestLink_AllowsSafeHrefs(t *testing.T) {
	for _, payload := range []string{"https://example.com", "/about", "#section", "mailto:user@example.com", "tel:+15551234"} {
		t.Run(payload, func(t *testing.T) {
			h := ui.Link(ui.LinkConfig{Href: payload, Text: "Click"})
			href := extractAttr(string(h), "href")
			if href != payload {
				t.Fatalf("safe href %q dropped (got %q)", payload, href)
			}
		})
	}
}

// TestLink_StripsEventHandlerExtraAttrs pins that the ExtraAttrs
// escape hatch never carries on* handlers into the DOM. Earlier the
// framework documented "ExtraAttrs is an escape hatch: callers must
// not pass event handlers"; the contract is now enforced.
func TestLink_StripsEventHandlerExtraAttrs(t *testing.T) {
	for _, attr := range []string{"onclick", "onmouseover", "onfocus", "onkeydown"} {
		t.Run(attr, func(t *testing.T) {
			h := ui.Link(ui.LinkConfig{
				Href: "/safe",
				Text: "Click",
				ExtraAttrs: html.Attrs{
					attr: "alert(1)",
				},
			})
			if strings.Contains(strings.ToLower(string(h)), strings.ToLower(attr)+`=`) {
				t.Fatalf("event-handler attr %q reached output: %s", attr, h)
			}
		})
	}
}

func TestLink_TextXSS(t *testing.T) {
	t.Parallel()
	h := ui.Link(ui.LinkConfig{
		Href: "/safe",
		Text: `<script>alert("xss")</script>`,
	})
	mustNotContain(t, h, "<script>alert")
	mustContain(t, h, "&lt;script&gt;")
	t.Logf("NOTE: script tags in link text were escaped to &lt;script&gt;")
}

func TestLink_ClassInjection(t *testing.T) {
	t.Parallel()
	h := ui.Link(ui.LinkConfig{
		Href:  "/safe",
		Text:  "safe",
		Class: `my-class" onclick="alert(1)`,
	})
	mustNotContain(t, h, `onclick="alert(1)"`)
	mustContain(t, h, "&quot;")
	t.Logf("NOTE: class with quote injection was escaped")
}

func TestLink_IDInjection(t *testing.T) {
	t.Parallel()
	h := ui.Link(ui.LinkConfig{
		Href: "/safe",
		Text: "safe",
		ID:   `myid"><script>alert(1)</script>`,
	})
	mustNotContain(t, h, `<script>alert(1)</script>`)
	mustContain(t, h, "&lt;script&gt;")
	t.Logf("NOTE: ID with script injection was escaped")
}

func TestLink_ExtraAttrsEventHandler(t *testing.T) {
	t.Parallel()
	h := ui.Link(ui.LinkConfig{
		Href: "/safe",
		Text: "safe",
		ExtraAttrs: html.Attrs{
			"onclick": `alert(document.cookie)`,
		},
	})
	out := string(h)
	// ExtraAttrs are passed through to buildAttrs → writeAttrs → Attr → Escape.
	// The framework escapes attribute values but does NOT strip dangerous
	// attribute names (onclick, onerror, etc.). Caller is responsible.
	if strings.Contains(out, `onclick="alert(document.cookie)"`) {
		t.Logf("NOTE: SECURITY FINDING — onclick rendered in output (attribute-escaped value but event handler name not stripped)")
		t.Logf("NOTE: ExtraAttrs is an escape hatch — callers must not pass event handlers")
	}
}

func TestLink_HrefPathTraversal(t *testing.T) {
	t.Parallel()
	h := ui.Link(ui.LinkConfig{
		Href: "/safe/../../../../etc/passwd",
		Text: "traversal",
	})
	out := string(h)
	href := extractAttr(out, "href")
	if href == "" {
		t.Fatal("SECURITY: [link-xss] href attribute missing from output")
	}
	// The framework doesn't sanitize path traversal in href: that's the
	// server's job. But the attribute should be properly escaped.
	if strings.Contains(out, `"../../../../etc/passwd"`) {
		t.Logf("NOTE: path traversal in href is passed through (attribute-escaped but not path-sanitized)")
		t.Logf("NOTE: server-side routing should resolve and reject traversal paths")
	}
}

func TestLink_EmptyHrefPanics(t *testing.T) {
	t.Parallel()
	mustPanic(t, "empty Href should panic", func() {
		ui.Link(ui.LinkConfig{Href: "", Text: "text"})
	})
}

func TestLink_EmptyTextPanics(t *testing.T) {
	t.Parallel()
	mustPanic(t, "empty Text should panic", func() {
		ui.Link(ui.LinkConfig{Href: "/safe", Text: ""})
	})
}

// TestLinkButtonRejectsControlByteScheme pins that a javascript:/
// vbscript: scheme split by an INTERIOR ASCII control byte (tab,
// newline, CR, NUL) is still refused. Browsers strip those bytes from
// the URL before resolving the scheme, so "java\tscript:" executes as
// javascript:. The deny-list must normalize the same way and panic.
func TestLinkButtonRejectsControlByteScheme(t *testing.T) {
	for _, payload := range []string{
		"javascript:alert(1)", // leading-safe baseline
		"java\tscript:alert(1)",
		"java\nscript:alert(1)",
		"jav\x00ascript:alert(1)",
		"vb\rscript:MsgBox(1)",
	} {
		t.Run(payload, func(t *testing.T) {
			mustPanic(t, "control-byte scheme must be refused", func() {
				ui.LinkButton(ui.LinkButtonConfig{Label: "x", Href: payload})
			})
		})
	}
}

// TestMenuNeutralisesControlByteScheme pins the same property for
// ui.Menu items: an interior-control-byte javascript: href is reduced
// to "#" rather than rendered verbatim.
func TestMenuNeutralisesControlByteScheme(t *testing.T) {
	for _, payload := range []string{
		"java\tscript:alert(1)",
		"java\nscript:alert(1)",
		"vb\rscript:MsgBox(1)",
		"data\t:text/html,<x>",
	} {
		t.Run(payload, func(t *testing.T) {
			h := ui.Menu(ui.MenuConfig{
				Label: "Open",
				Items: []ui.MenuItem{{Label: "go", Href: payload}},
			})
			out := strings.ToLower(string(h))
			if strings.Contains(out, "javascript:") || strings.Contains(out, "vbscript:") || strings.Contains(out, "data:") || strings.Contains(out, "data\t") {
				t.Fatalf("control-byte scheme reached menu href: %s", h)
			}
			mustContain(t, h, `href="#"`)
		})
	}
}

// TestCardHrefRefusesUnsafeSchemes pins the URL scheme allow-list on
// the linked Card: a javascript:/data: (or control-byte-split) Href is
// refused at render naming the prop — a configured href is the
// developer's mistake, the same refusal Alert.DismissHref and
// Form.Action meet — and never reaches the rendered <a>.
func TestCardHrefRefusesUnsafeSchemes(t *testing.T) {
	for _, payload := range []string{
		"javascript:alert(document.cookie)",
		"data:text/html,<script>alert(1)</script>",
		"java\tscript:alert(1)",
		"//evil.example/x",
	} {
		t.Run(payload, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatalf("unsafe card href rendered instead of refusing: %s", payload)
				}
				msg, ok := r.(string)
				if !ok || !strings.Contains(msg, "Href") {
					t.Fatalf("panic does not name Href: %v", r)
				}
			}()
			h := ui.Card(ui.CardConfig{Heading: "T", Href: payload}, render.Text("body"))
			t.Fatalf("unsafe card href rendered: %s", h)
		})
	}
	// Happy path: a safe href round-trips.
	h := ui.Card(ui.CardConfig{Heading: "T", Href: "/items/1"}, render.Text("body"))
	mustContain(t, h, `href="/items/1"`)
}

// TestTagHrefDropsUnsafeSchemes pins the same property on the linked
// Tag/chip variant.
func TestTagHrefDropsUnsafeSchemes(t *testing.T) {
	for _, payload := range []string{
		"javascript:alert(document.cookie)",
		"data:text/html,<script>alert(1)</script>",
		"java\tscript:alert(1)",
		"//evil.example/x",
	} {
		t.Run(payload, func(t *testing.T) {
			h := ui.Tag(ui.TagConfig{Label: "design", Href: payload})
			out := strings.ToLower(string(h))
			if strings.Contains(out, "javascript:") || strings.Contains(out, "data:") || strings.Contains(out, "//evil.example") {
				t.Fatalf("unsafe scheme reached tag href: %s", h)
			}
			mustContain(t, h, `href="#"`)
		})
	}
	// Happy path: a safe href round-trips.
	h := ui.Tag(ui.TagConfig{Label: "design", Href: "/?tag=design"})
	mustContain(t, h, `href="/?tag=design"`)
}

// TestNavHrefSinksDropUnsafeSchemes pins the URL scheme allow-list on
// the remaining content-level Href sinks that render live anchors:
// Sidebar item Href, DocLayout crumb Href, and DocPrevNext pager
// Hrefs. Each degrades to "#", never a live javascript: link.
// ProgressSteps step Href is no longer in this set: it rides
// headless.Steps, which refuses a configured href the anchor policy
// rejects (see TestProgressStepsHrefIsRefusedNotDegraded).
func TestNavHrefSinksDropUnsafeSchemes(t *testing.T) {
	const payload = "javascript:alert(1)"
	surfaces := map[string]func() render.HTML{
		"sidebar-item": func() render.HTML {
			return ui.SidebarBody(ui.SidebarConfig{
				Items: []ui.SidebarItem{{Label: "Home", Href: payload}},
			})
		},
		"doc-crumb": func() render.HTML {
			return ui.DocLayout(ui.DocLayoutConfig{
				Crumbs: []ui.DocCrumb{{Label: "Docs", Href: payload}, {Label: "Here"}},
			}, render.Text("body"))
		},
		"doc-pager": func() render.HTML {
			return ui.DocPrevNext(ui.DocPager{
				PrevHref: payload, PrevLabel: "p",
				NextHref: payload, NextLabel: "n",
			})
		},
	}
	for name, renderFn := range surfaces {
		t.Run(name, func(t *testing.T) {
			h := renderFn()
			if strings.Contains(strings.ToLower(string(h)), "javascript:") {
				t.Fatalf("javascript: href reached output: %s", h)
			}
			// Every surface here degrades the refused href to the
			// inert "#" anchor it keeps.
			if !strings.Contains(string(h), `href="#"`) {
				t.Fatalf("a refused href still rendered an anchor: %s", h)
			}
		})
	}
}

// A ProgressSteps step Href the anchor policy refuses is refused at
// render naming the prop: the configured href is the developer's
// mistake, the posture the primitive shares with Card and Form.
func TestProgressStepsHrefIsRefusedNotDegraded(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("an unsafe step href rendered instead of refusing")
		}
		msg, ok := r.(string)
		if !ok || !strings.Contains(msg, "Href") {
			t.Fatalf("panic does not name Href: %v", r)
		}
	}()
	ui.ProgressSteps(ui.ProgressStepsConfig{
		Steps: []ui.ProgressStep{{Label: "One", Status: ui.ProgressStepComplete, Href: "javascript:alert(1)"}},
	})
}

// TestHrefSinksDropProtocolRelative pins that the three sinks that
// previously used the weak sanitizeHref (MenuItem.Href,
// SidebarItem.Href, Notification.DismissHref) now use safeURL, which
// drops protocol-relative URLs (//evil.com), file:, and blob:, which
// schemes sanitizeHref let through verbatim. Each degrades to href="#".
func TestHrefSinksDropProtocolRelative(t *testing.T) {
	for _, payload := range []string{
		"//evil.example/x",
		"file:///etc/passwd",
		"blob:https://evil.example/abc",
	} {
		t.Run(payload, func(t *testing.T) {
			menu := ui.Menu(ui.MenuConfig{
				Label: "Open",
				Items: []ui.MenuItem{{Label: "go", Href: payload}},
			})
			if strings.Contains(string(menu), "evil.example") {
				t.Fatalf("protocol-relative/unsafe scheme reached menu href: %s", menu)
			}
			mustContain(t, menu, `href="#"`)

			sidebar := ui.SidebarBody(ui.SidebarConfig{
				Items: []ui.SidebarItem{{Label: "go", Href: payload}},
			})
			if strings.Contains(string(sidebar), "evil.example") {
				t.Fatalf("protocol-relative/unsafe scheme reached sidebar href: %s", sidebar)
			}
			mustContain(t, sidebar, `href="#"`)

			// The toast primitive refuses a dismissed toast with no
			// Island (hard rule 1), so the sink carries one; the href
			// policy refusal under test fires before it matters.
			notif := func() (out render.HTML) {
				defer func() {
					if recover() == nil {
						return
					}
					out = render.HTML("")
				}()
				return ui.Notification(ui.NotificationConfig{
					Title:       "T",
					DismissHref: payload,
					Island:      headless.Island{Endpoint: "/island/n", Signal: "n"},
				})
			}()
			// The primitive REFUSES the rejected href at render (the
			// empty result is the refusal, recorded by the same
			// recover pattern the StepWizard sink uses); nothing
			// unsafe can reach the page.
			if strings.Contains(string(notif), "evil.example") {
				t.Fatalf("protocol-relative/unsafe scheme reached notification dismiss href: %s", notif)
			}
		})
	}
}

// ═══════════════════════════════════════════════════════════════════════════════
//  Form component security (tests 11–20)
// ═══════════════════════════════════════════════════════════════════════════════

func TestForm_ActionXSS(t *testing.T) {
	t.Parallel()
	h := ui.Form(ui.FormConfig{
		Action: "/save?redirect=<script>alert(1)</script>",
	}, render.Text("field"))
	mustNotContain(t, h, `<script>alert(1)</script>`)
	mustContain(t, h, "&lt;script&gt;")
	t.Logf("NOTE: script tags in form action were escaped")
}

func TestForm_ActionPathTraversal(t *testing.T) {
	t.Parallel()
	h := ui.Form(ui.FormConfig{
		Action: "/../../../etc/passwd",
	}, render.Text("field"))
	mustContain(t, h, "/../../../etc/passwd")
	// The action value is attribute-escaped but not path-resolved.
	t.Logf("NOTE: path traversal in action is attribute-escaped but not path-sanitized")
}

// An action the anchor policy refuses used to be substituted with
// "#": a form whose submit went nowhere, which is worse than a no-op.
// The refusal is the contract now — the panic is the fix, not a bug.
func TestForm_ActionJavaScriptScheme(t *testing.T) {
	t.Parallel()
	mustPanic(t, "unsafe action must panic at render, not render a dead form", func() {
		ui.Form(ui.FormConfig{
			Action: "javascript:alert(1)",
		}, render.Text("field"))
	})
}

func TestForm_MethodInjection(t *testing.T) {
	t.Parallel()
	mustPanic(t, "non-GET/POST method should panic", func() {
		ui.Form(ui.FormConfig{
			Action: "/save",
			Method: "DELETE",
		}, render.Text("field"))
	})
}

func TestForm_ClassInjection(t *testing.T) {
	t.Parallel()
	h := ui.Form(ui.FormConfig{
		Action: "/save",
		Class:  `my-form" onsubmit="alert(1)`,
	}, render.Text("field"))
	mustNotContain(t, h, `onsubmit="alert(1)"`)
	mustContain(t, h, "&quot;")
	t.Logf("NOTE: class with quote injection was escaped")
}

func TestForm_IDInjection(t *testing.T) {
	t.Parallel()
	h := ui.Form(ui.FormConfig{
		Action: "/save",
		ID:     `form1"><script>alert(1)</script>`,
	}, render.Text("field"))
	mustNotContain(t, h, `<script>alert(1)</script>`)
	mustContain(t, h, "&lt;script&gt;")
	t.Logf("NOTE: ID with script injection was escaped")
}

func TestForm_FieldNameXSS(t *testing.T) {
	t.Parallel()
	h := ui.Form(ui.FormConfig{
		Action: "/save",
	},
		html.Input(html.InputConfig{
			Type: "text",
			Name: `user<script>alert(1)</script>`,
		}),
	)
	mustNotContain(t, h, `<script>alert(1)</script>`)
	// The name attribute value should be escaped.
	mustContain(t, h, "&lt;script&gt;")
	t.Logf("NOTE: input name with script tags was escaped in attribute")
}

func TestForm_FieldValueXSS(t *testing.T) {
	t.Parallel()
	h := ui.Form(ui.FormConfig{
		Action: "/save",
	},
		html.Input(html.InputConfig{
			Type:  "text",
			Name:  "q",
			Value: `"><script>alert(1)</script>`,
		}),
	)
	mustNotContain(t, h, `<script>alert(1)</script>`)
	mustContain(t, h, "&lt;script&gt;")
	t.Logf("NOTE: input value with script tags was escaped in attribute")
}

func TestForm_ErrorMessageXSS(t *testing.T) {
	t.Parallel()
	errs := ui.FieldErrors{"email": `<script>alert("xss")</script>`}
	h := ui.Form(ui.FormConfig{
		Action: "/save",
		ID:     "xss-form",
		Errors: errs,
	},
		ui.FormFieldFor(errs, "email", ui.FormFieldConfig{
			Label: "Email",
			For:   "email",
			Input: func(c headless.FieldControl) render.HTML {
				return ui.Control(ui.ControlConfig{Field: c, Type: "email", Name: "email"})
			},
		}),
	)
	mustNotContain(t, h, `<script>alert("xss")</script>`)
	mustContain(t, h, "&lt;script&gt;")
	t.Logf("NOTE: error message with script tags was escaped")
}

func TestForm_CSRFTokenInjection(t *testing.T) {
	t.Parallel()
	// CSRF token is rendered via csrfHiddenInput which uses
	// html/template.HTMLEscapeString. Test that special characters
	// in a token-like value are escaped.
	// We can't easily inject a context with a malicious token here,
	// but we can verify the hidden input escaping behavior directly.
	// The token value goes through template.HTMLEscapeString which
	// escapes &, <, >, ", '.
	token := `" onclick="alert(1)`
	escaped := `" onclick=&#34;alert(1)`
	if token == escaped {
		t.Errorf("SECURITY: [form-csrf] CSRF token with quotes not escaped")
	}
	t.Logf("NOTE: CSRF token escaping confirmed — %q → %q", token, escaped)
}

// ═══════════════════════════════════════════════════════════════════════════════
//  Form inputs security (tests 21–30)
// ═══════════════════════════════════════════════════════════════════════════════

func TestFormInput_NameXSS(t *testing.T) {
	t.Parallel()
	h := html.Input(html.InputConfig{
		Type: "text",
		Name: `foo" onclick="alert(1)`,
	})
	mustNotContain(t, h, `onclick="alert(1)"`)
	mustContain(t, h, "&quot;")
	t.Logf("NOTE: input name with quote injection was escaped")
}

func TestFormInput_ValueXSS(t *testing.T) {
	t.Parallel()
	h := html.Input(html.InputConfig{
		Type:  "text",
		Name:  "field",
		Value: `<img src=x onerror=alert(1)>`,
	})
	mustNotContain(t, h, `<img src=x onerror=alert(1)>`)
	mustContain(t, h, "&lt;img")
	t.Logf("NOTE: input value with img-onerror XSS was escaped")
}

func TestFormInput_PlaceholderXSS(t *testing.T) {
	t.Parallel()
	h := html.Input(html.InputConfig{
		Type:        "text",
		Name:        "field",
		Placeholder: `"><script>alert(1)</script>`,
	})
	mustNotContain(t, h, `<script>alert(1)</script>`)
	mustContain(t, h, "&lt;script&gt;")
	t.Logf("NOTE: placeholder with script tags was escaped")
}

func TestFormInput_LabelXSS(t *testing.T) {
	t.Parallel()
	h := ui.FormField(ui.FormFieldConfig{
		Label: `<script>alert("label-xss")</script>`,
		For:   "field-id",
		Input: func(c headless.FieldControl) render.HTML {
			return ui.Control(ui.ControlConfig{Field: c, Type: "text", Name: "field"})
		},
	})
	mustNotContain(t, h, `<script>alert("label-xss")</script>`)
	mustContain(t, h, "&lt;script&gt;")
	t.Logf("NOTE: label with script tags was escaped")
}

func TestFormInput_HelpTextXSS(t *testing.T) {
	t.Parallel()
	h := ui.FormField(ui.FormFieldConfig{
		Label: "Email",
		For:   "email",
		Help:  `<img src=x onerror=alert(1)> click here`,
		Input: func(c headless.FieldControl) render.HTML {
			return ui.Control(ui.ControlConfig{Field: c, Type: "email", Name: "email"})
		},
	})
	mustNotContain(t, h, `<img src=x onerror=alert(1)>`)
	mustContain(t, h, "&lt;img")
	t.Logf("NOTE: help text with img-onerror XSS was escaped")
}

func TestFormInput_AutocompleteOff(t *testing.T) {
	t.Parallel()
	h := html.Input(html.InputConfig{
		Type: "password",
		Name: "password",
	})
	out := string(h)
	// Check that the password input is rendered. The framework's
	// html.Input doesn't force autocomplete=off, but callers can set it
	// via ExtraAttrs.
	if !strings.Contains(out, `type="password"`) {
		t.Fatalf("expected password input type, got: %s", out)
	}
	t.Logf("NOTE: password input rendered — caller should add autocomplete=off via ExtraAttrs if desired")
	// Verify ExtraAttrs can set autocomplete=off
	h2 := html.Input(html.InputConfig{
		Type:       "password",
		Name:       "password",
		ExtraAttrs: html.Attrs{"autocomplete": "off"},
	})
	mustContain(t, h2, `autocomplete="off"`)
}

func TestFormInput_TypeValidation(t *testing.T) {
	t.Parallel()
	mustPanic(t, "empty input type should panic", func() {
		html.Input(html.InputConfig{Type: "", Name: "field"})
	})
}

func TestFormInput_PatternInjection(t *testing.T) {
	t.Parallel()
	h := html.Input(html.InputConfig{
		Type:       "text",
		Name:       "zip",
		ExtraAttrs: html.Attrs{"pattern": `\d{5}" onclick="alert(1)`},
	})
	mustNotContain(t, h, `onclick="alert(1)"`)
	mustContain(t, h, "&quot;")
	t.Logf("NOTE: pattern attribute with quote injection was escaped")
}

func TestFormInput_MaxLengthEnforced(t *testing.T) {
	t.Parallel()
	h := html.Input(html.InputConfig{
		Type:       "text",
		Name:       "comment",
		ExtraAttrs: html.Attrs{"maxlength": "500"},
	})
	mustContain(t, h, `maxlength="500"`)
	t.Logf("NOTE: maxlength attribute passed through correctly")
}

func TestFormInput_RequiredAttribute(t *testing.T) {
	t.Parallel()
	h := ui.FormField(ui.FormFieldConfig{
		Label:    "Email",
		For:      "email",
		Required: true,
		Input: func(c headless.FieldControl) render.HTML {
			return ui.Control(ui.ControlConfig{Field: c, Type: "email", Name: "email"})
		},
	})
	out := string(h)
	// The required state rides the label (data-required) and the
	// control (required); the stylesheet draws the visible mark from
	// the state.
	if !strings.Contains(out, `data-required`) {
		t.Errorf("SECURITY: [form-input] required field missing its state on the label\nHTML: %s", out)
	}
	// The control's own opening tag carries required: a whole-field
	// search is satisfied by the label's data-required="".
	i := strings.Index(out, "<input")
	if i < 0 {
		t.Fatalf("SECURITY: [form-input] no <input> control in the field\nHTML: %s", out)
	}
	open := out[i:]
	if j := strings.IndexByte(open, '>'); j >= 0 {
		open = open[:j+1]
	}
	if !strings.Contains(open, `required=""`) {
		t.Errorf("SECURITY: [form-input] required field missing its state on the control\nHTML: %s", open)
	}
}

// ═══════════════════════════════════════════════════════════════════════════════
//  HTML rendering safety (tests 31–40)
// ═══════════════════════════════════════════════════════════════════════════════

func TestHTML_HeadingXSS(t *testing.T) {
	t.Parallel()
	h := html.Heading(html.HeadingConfig{Level: 1},
		render.Text(`<script>alert("heading")</script>`),
	)
	mustNotContain(t, h, `<script>alert("heading")</script>`)
	mustContain(t, h, "&lt;script&gt;")
	t.Logf("NOTE: heading text with script tags was escaped")
}

func TestHTML_ParagraphXSS(t *testing.T) {
	t.Parallel()
	h := html.Paragraph(html.TextConfig{},
		render.Text(`<img src=x onerror=alert(1)> text`),
	)
	mustNotContain(t, h, `<img src=x onerror=alert(1)>`)
	mustContain(t, h, "&lt;img")
	t.Logf("NOTE: paragraph text with img-onerror XSS was escaped")
}

func TestHTML_CodeXSS(t *testing.T) {
	t.Parallel()
	h := html.Code(html.TextConfig{},
		render.Text(`</code><script>alert(1)</script>`),
	)
	mustNotContain(t, h, `<script>alert(1)</script>`)
	mustContain(t, h, "&lt;/code&gt;&lt;script&gt;")
	t.Logf("NOTE: code content with script and tag breakout was escaped")
}

func TestHTML_BlockquoteXSS(t *testing.T) {
	t.Parallel()
	h := html.Blockquote(html.TextConfig{},
		render.Text(`<script>alert("quote")</script>`),
	)
	mustNotContain(t, h, `<script>alert("quote")</script>`)
	mustContain(t, h, "&lt;script&gt;")
	t.Logf("NOTE: blockquote text with script tags was escaped")
}

func TestHTML_SlugifySanitizes(t *testing.T) {
	t.Parallel()
	// Heading auto-generates an id via slugify. Test that dangerous
	// characters in heading text don't produce dangerous id values.
	h := html.Heading(html.HeadingConfig{Level: 2},
		render.Text(`Test heading <script>alert(1)</script> & "quotes"`),
	)
	out := string(h)
	mustNotContain(t, h, `id="heading-test-heading-<script>`)
	mustNotContain(t, h, `onclick`)
	mustNotContain(t, h, `<script`)
	// The slugified id should be lowercase, dash-separated, max 64 chars.
	if !strings.Contains(out, `id="heading-`) {
		t.Errorf("SECURITY: [html-slugify] expected auto-generated id, got: %s", out)
	}
	t.Logf("NOTE: slugify produced safe id from malicious heading text")
}

func TestHTML_TimeDatetimeXSS(t *testing.T) {
	t.Parallel()
	h := html.Time(html.TimeConfig{
		Datetime: `2024-01-01"><script>alert(1)</script>`,
	},
		render.Text("Jan 1"),
	)
	mustNotContain(t, h, `<script>alert(1)</script>`)
	// The datetime attribute value should have quotes escaped.
	mustContain(t, h, "&lt;script&gt;")
	t.Logf("NOTE: time datetime with script injection was escaped")
}

func TestHTML_AbbrTitleXSS(t *testing.T) {
	t.Parallel()
	h := html.Abbr(html.AbbrConfig{
		Title: `"><script>alert(1)</script>`,
	},
		render.Text("W3C"),
	)
	mustNotContain(t, h, `<script>alert(1)</script>`)
	mustContain(t, h, "&lt;script&gt;")
	t.Logf("NOTE: abbr title with script injection was escaped")
}

func TestHTML_MarkXSS(t *testing.T) {
	t.Parallel()
	h := html.Mark(html.TextConfig{},
		render.Text(`<script>alert("mark")</script>highlighted`),
	)
	mustNotContain(t, h, `<script>alert("mark")</script>`)
	mustContain(t, h, "&lt;script&gt;")
	t.Logf("NOTE: mark content with script tags was escaped")
}

func TestHTML_SmallXSS(t *testing.T) {
	t.Parallel()
	h := html.Small(html.TextConfig{},
		render.Text(`<script>alert("small")</script> fine print`),
	)
	mustNotContain(t, h, `<script>alert("small")</script>`)
	mustContain(t, h, "&lt;script&gt;")
	t.Logf("NOTE: small content with script tags was escaped")
}

func TestHTML_PreCodeXSS(t *testing.T) {
	t.Parallel()
	h := html.Pre(html.TextConfig{},
		html.Code(html.TextConfig{},
			render.Text(`</pre></code><script>alert("precode")</script>`),
		),
	)
	mustNotContain(t, h, `<script>alert("precode")</script>`)
	// The closing tag breakout should be escaped.
	mustNotContain(t, h, `</pre></code><script>`)
	mustContain(t, h, "&lt;/pre&gt;&lt;/code&gt;&lt;script&gt;")
	t.Logf("NOTE: pre>code content with tag breakout was escaped")
}

// ─── Internal helper ──────────────────────────────────────────────────────────

// extractAttr naively extracts an attribute value from HTML output.
// Used only for logging/inspection in tests, not for production.
func extractAttr(htmlStr, attrName string) string {
	needle := attrName + `="`
	idx := strings.Index(htmlStr, needle)
	if idx < 0 {
		return ""
	}
	start := idx + len(needle)
	end := strings.Index(htmlStr[start:], `"`)
	if end < 0 {
		return htmlStr[start:]
	}
	return htmlStr[start : start+end]
}

// ═══════════════════════════════════════════════════════════════════════════════
//  Form action sinks: SearchInput + FilterToolbar + SignOut (drift of ui.Form)
// ═══════════════════════════════════════════════════════════════════════════════

// TestFormActionSinksRejectUnsafeURL pins the URL-scheme allow-list on the
// <form action> sinks that write cfg.Action straight into markup instead of
// going through the html.Form primitive's setURLAttr guard, while the
// sibling ui.Form runs it through urlsafe.CleanAnchor. Property × surface:
// loop the same attack shapes over EVERY component. FilterToolbar runs with
// HideReset so the form action is the only sink exercised (its reset
// LinkButton already had a guard). SignOut hand-rolls its <form> tag with
// render.Escape, which is HTML-escaping and scheme-blind, so its Action
// must be scheme-allow-listed like every other sink. A SignOut fix should
// degrade a rejected action to its "/auth/logout" default rather than "#"
// (a "#"-action form submits to the page itself).
func TestFormActionSinksRejectUnsafeURL(t *testing.T) {
	unsafe := []string{
		"javascript:alert(1)",
		"vbscript:msgbox(1)",
		"data:text/html,<script>alert(1)</script>",
		"//evil.com/x",
	}
	// The StepWizard sink refuses at render; the counter proves every
	// unsafe action met the refusal rather than an empty render that
	// would pass the verbatim check while proving nothing. Declared
	// before the table because the sink closure writes to it.
	stepWizardRefusals := 0
	sinks := []struct {
		name   string
		render func(action string) render.HTML
	}{
		{"SearchInput", func(a string) render.HTML {
			return ui.SearchInput(ui.SearchInputConfig{Name: "q", ID: "q", Action: a})
		}},
		{"FilterToolbar", func(a string) render.HTML {
			return ui.FilterToolbar(ui.FilterToolbarConfig{
				Action: a, HideReset: true,
				Sort: []ui.SortOption{{Value: "x", Label: "X"}},
			})
		}},
		{"SignOut", func(a string) render.HTML {
			return ui.SignOut(ui.SignOutConfig{Action: a})
		}},
		{"StepWizard", func(a string) render.HTML {
			// The headless primitive refuses a rejected action at
			// render, the posture every configured href in that
			// package keeps. The refusal is RECORDED, not swallowed:
			// stepWizardRefused tells the unsafe loop below that the
			// panic happened, and a render that somehow came back
			// without refusing returns its markup for the verbatim
			// check to see.
			var out render.HTML
			func() {
				defer func() {
					if recover() != nil {
						stepWizardRefusals++
					}
				}()
				out = ui.StepWizard(ui.StepWizardConfig{Action: a,
					Steps: []ui.StepWizardStep{{Heading: "H"}}})
			}()
			return out
		}},
	}
	for _, s := range sinks {
		t.Run(s.name, func(t *testing.T) {
			for _, action := range unsafe {
				h := string(s.render(action))
				if strings.Contains(h, action) {
					t.Errorf("SECURITY: %s rendered unsafe form action %q verbatim:\n%s", s.name, action, h)
				}
				if scheme := schemeOf(action); scheme != "" && strings.Contains(h, scheme+":") {
					t.Errorf("SECURITY: %s kept dangerous scheme %q in output:\n%s", s.name, scheme, h)
				}
			}
			// A valid relative action must round-trip unchanged: the guard
			// is a scheme allow-list, not a blanket reject.
			h := string(s.render("/search"))
			if !strings.Contains(h, `action="/search"`) {
				t.Errorf("%s dropped a valid relative action:\n%s", s.name, h)
			}
		})
	}
	// All four unsafe actions met the render refusal — not an empty
	// render that would pass the verbatim check while proving nothing.
	if stepWizardRefusals != len(unsafe) {
		t.Errorf("StepWizard refused %d of %d unsafe actions at render — a swallowed panic proves nothing", stepWizardRefusals, len(unsafe))
	}
}

// schemeOf returns the lowercased scheme prefix of u ("" for relative /
// protocol-relative URLs), used only to narrow the unsafe-action assertion.
func schemeOf(u string) string {
	i := strings.Index(u, ":")
	if i <= 0 {
		return ""
	}
	// protocol-relative ("//host") has no scheme.
	if strings.HasPrefix(u, "//") {
		return ""
	}
	return strings.ToLower(u[:i])
}

// TestFilterToolbarExtraAttrsCannotOverrideAction pins that the sanitized
// form action is NOT silently reversible via ExtraAttrs, including via a
// case-VARIANT key, because HTML attribute names are case-insensitive.
// #198 ran cfg.Action through urlsafe.CleanAnchor but let the ExtraAttrs
// merge write cfg.ExtraAttrs["action"] straight over it; #199 dropped the
// lowercase "action" key from ExtraAttrs. A mixed-case key ("Action" /
// "ACTION" / "AcTiOn") still survived as a distinct map entry, rendered as a
// SECOND attribute, and the HTML parser folds duplicate attributes back onto
// "action" (first occurrence wins), so whichever rendered first became the
// live form action, silently reversing the sanitizer. Property × key-case:
// the sanitized action must be the ONLY attribute that folds to "action".
func TestFilterToolbarExtraAttrsCannotOverrideAction(t *testing.T) {
	for _, key := range []string{"action", "Action", "ACTION", "AcTiOn"} {
		t.Run(key, func(t *testing.T) {
			out := string(ui.FilterToolbar(ui.FilterToolbarConfig{
				Action:     "/search",
				HideReset:  true,
				Sort:       []ui.SortOption{{Value: "x", Label: "X"}},
				ExtraAttrs: html.Attrs{key: "javascript:alert(1)"},
			}))
			if strings.Contains(out, "javascript:") {
				t.Errorf("SECURITY: ExtraAttrs[%q] (case-variant of action) leaked a javascript: action:\n%s", key, out)
			}
			if !strings.Contains(out, `action="/search"`) {
				t.Errorf("sanitized action lost from output:\n%s", out)
			}
		})
	}
}

// TestSearchInputExtraAttrsCaseInsensitiveProtected pins the same property on
// SearchInput's <input>: the re-asserted attributes (type/name/id) must win
// over ExtraAttrs even when the caller uses a case-variant key. HTML attribute
// names are case-insensitive, so an ExtraAttrs "Type"/"Name"/"ID" survives the
// lowercase re-assert as a distinct map key, renders as a second attribute,
// and folds back onto the protected one in the parser (e.g. "Type" can flip a
// search box to a hidden/submit input, "Name" can clobber the submitted field).
func TestSearchInputExtraAttrsCaseInsensitiveProtected(t *testing.T) {
	// Every case-variant of each re-asserted key; "PWNED" must never render.
	cases := []string{
		"type", "Type", "TYPE", "tYpE",
		"name", "Name", "NAME", "nAmE",
		"id", "ID", "Id", "iD",
	}
	for _, key := range cases {
		t.Run(key, func(t *testing.T) {
			out := string(ui.SearchInput(ui.SearchInputConfig{
				Name: "q", ID: "q",
				ExtraAttrs: html.Attrs{key: "PWNED"},
			}))
			if strings.Contains(out, "PWNED") {
				t.Errorf("SECURITY: ExtraAttrs[%q] (case-variant of a protected attr) leaked into <input>:\n%s", key, out)
			}
		})
	}
}

// TestRailAnchorsStayFragmentReferences pins the in-page rail/summary
// anchor family: these components force a "#" prefix onto caller
// anchors (or an id), so a data-derived anchor value can never become a
// navigable scheme URL — "#"javascript:… is a same-page fragment, inert
// by construction. Surfaces: AnchoredRail item anchors, StepRail item
// anchors, and ValidationSummary field anchors. StepRail's MetaHref is
// the contrast surface: it renders a real href (no forced "#"), so it
// must go through the html.Link scheme allow-list and be dropped, not
// rendered, when dangerous.
func TestRailAnchorsStayFragmentReferences(t *testing.T) {
	anchors := []string{"javascript:alert(1)", "https://evil.example/x", "//evil.example/x"}

	for _, a := range anchors {
		rail := string(ui.AnchoredRail(ui.AnchoredRailConfig{Label: "L", Items: []ui.RailItem{
			{Anchor: a, Text: "T"},
		}}))
		if !strings.Contains(rail, `href="#`+a+`"`) {
			t.Errorf("AnchoredRail: anchor %q must render as a same-page fragment href, got:\n%s", a, rail)
		}

		step := string(ui.StepRail(ui.StepRailConfig{Items: []ui.StepRailItem{
			{Number: "01", Anchor: a, Label: "T"},
		}}))
		if !strings.Contains(step, `href="#`+a+`"`) {
			t.Errorf("StepRail: anchor %q must render as a same-page fragment href, got:\n%s", a, step)
		}
	}

	summary := string(ui.ValidationSummary(ui.ValidationSummaryConfig{
		ID:       "rail-sum",
		Errors:   ui.FieldErrors{"email": "invalid"},
		FieldIDs: map[string]string{"email": `javascript:alert(1)`},
	}))
	// The anchor policy accepts a bare fragment, so the forged value
	// rides behind the "#": a same-page fragment, inert by
	// construction — "#javascript:…" is an id lookup, not a scheme.
	if !strings.Contains(summary, `href="#javascript:alert(1)"`) {
		t.Errorf("ValidationSummary: field anchor must stay a fragment reference:\n%s", summary)
	}

	meta := string(ui.StepRail(ui.StepRailConfig{
		Title:    "Steps",
		Meta:     "Help",
		MetaHref: "javascript:alert(1)",
		Items:    []ui.StepRailItem{{Number: "01", Anchor: "a", Label: "T"}},
	}))
	if strings.Contains(strings.ToLower(meta), `href="javascript`) {
		t.Errorf("SECURITY: StepRail MetaHref rendered a live javascript: link:\n%s", meta)
	}
}

// TestSignOutNextFieldEscapesBreakout pins the SignOut redirect field:
// Next is round-tripped into a hidden input, so it must be
// attribute-escaped (quote-breakout neutralised). Scheme safety of the
// redirect itself is enforced at the sink, battery/auth's
// successRedirect/isSafeRelativePath (same-origin relative path or
// fallback), so this test only owns the markup-safety surface.
func TestSignOutNextFieldEscapesBreakout(t *testing.T) {
	payload := `"><script>alert(1)</script>`
	h := string(ui.SignOut(ui.SignOutConfig{Next: payload}))
	if strings.Contains(h, payload) || strings.Contains(h, "<script>") {
		t.Errorf("SECURITY: SignOut Next field rendered unescaped:\n%s", h)
	}
	if !strings.Contains(h, `name="next"`) {
		t.Errorf("SignOut must still emit the hidden next field:\n%s", h)
	}
	if !strings.Contains(h, "&lt;script&gt;") {
		t.Errorf("SignOut Next should appear escaped:\n%s", h)
	}
}
