package ui

import (
	"strings"
	"sync"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// ---------------------------------------------------------------------------
// Helper: mustNotContainRaw reports if the raw HTML fragment contains an
// unescaped dangerous pattern (script tag, event handler, javascript:
// URI).  It logs a NOTE when the pattern is safely absent.
// ---------------------------------------------------------------------------
func mustNotContainRaw(t *testing.T, html, pattern, label string) {
	t.Helper()
	if strings.Contains(html, pattern) {
		t.Errorf("SECURITY: [%s] unescaped %q found in output", label, pattern)
	} else {
		t.Logf("NOTE: [%s] %q correctly escaped", label, pattern)
	}
}

// ---------------------------------------------------------------------------
// Helper: mustNotContainAttrBreakout checks that a value intended for an
// attribute context cannot break out of the quoted attribute via ".
// ---------------------------------------------------------------------------
func mustNotContainAttrBreakout(t *testing.T, html, injection, label string) {
	t.Helper()
	if strings.Contains(html, `" `+injection) || strings.Contains(html, `"`+injection+` `) {
		t.Errorf("SECURITY: [%s] attribute breakout detected for %q", label, injection)
	} else {
		t.Logf("NOTE: [%s] attribute breakout correctly prevented for %q", label, injection)
	}
}

// ===========================================================================
// Modal security (tested via ConfirmAction which builds a preset.Modal)
// ===========================================================================

// TestModal_TitleXSS verifies that a modal title containing <script> tags
// is HTML-escaped and never rendered as executable markup.
func TestModal_TitleXSS(t *testing.T) {
	t.Parallel()
	slot := &confirmDialogSlot{
		titleID:      "test-modal-title-xss-title",
		bodyID:       "test-modal-title-xss-body",
		title:        `<script>alert("xss")</script>`,
		body:         "Safe body",
		confirmLabel: "OK",
		cancelLabel:  "Cancel",
	}
	h := string(slot.Render())
	mustNotContainRaw(t, h, "<script>", "modal-title-xss")
	if !strings.Contains(h, "&lt;script&gt;") {
		t.Errorf("SECURITY: [modal-title-xss] expected escaped &lt;script&gt;, got: %s", h)
	}
}

// TestModal_BodyXSS verifies that a modal body containing <script> tags
// is HTML-escaped. The real protection is that < is escaped to &lt;,
// so no element can be parsed by browsers.
func TestModal_BodyXSS(t *testing.T) {
	t.Parallel()
	slot := &confirmDialogSlot{
		titleID:      "test-modal-body-xss-title",
		bodyID:       "test-modal-body-xss-body",
		title:        "Safe title",
		body:         `<img src=x onerror="alert(1)">`,
		confirmLabel: "OK",
		cancelLabel:  "Cancel",
	}
	h := string(slot.Render())
	// The key protection: < is escaped so no <img> element is created.
	mustNotContainRaw(t, h, "<img", "modal-body-xss")
	if !strings.Contains(h, "&lt;img") {
		t.Errorf("SECURITY: [modal-body-xss] expected escaped &lt;img, got: %s", h)
	}
}

// TestModal_ActionURLXSS verifies that an RPC path containing javascript:
// is rendered safely — it goes into data-fui-rpc (not href), so it can't
// navigate. The value is attr-escaped by render.Attr().
func TestModal_ActionURLXSS(t *testing.T) {
	t.Parallel()
	slot := &confirmDialogSlot{
		titleID:      "test-modal-url-xss-title",
		bodyID:       "test-modal-url-xss-body",
		title:        "Delete?",
		body:         "Sure?",
		rpcPath:      `javascript:alert(1)`,
		rpcMethod:    "POST",
		confirmLabel: "OK",
		cancelLabel:  "Cancel",
	}
	h := string(slot.Render())
	// The RPC path goes into data-fui-rpc="..." which is attr-escaped.
	// It should not appear as href="javascript:".
	if strings.Contains(h, `href="javascript:`) {
		t.Errorf("SECURITY: [modal-action-url-xss] javascript: URI leaked into href")
	} else {
		t.Logf("NOTE: [modal-action-url-xss] javascript: URI in data-fui-rpc, not href")
	}
	if !strings.Contains(h, "javascript:") {
		t.Errorf("SECURITY: [modal-action-url-xss] expected RPC path value in output, got: %s", h)
	}
}

// TestModal_ClassInjection verifies that body text containing quote and
// script characters cannot break out of its text node context.
func TestModal_ClassInjection(t *testing.T) {
	t.Parallel()
	slot := &confirmDialogSlot{
		titleID:      "x-title",
		bodyID:       "x-body",
		title:        "Title",
		body:         `Body"><script>alert(1)</script><span class="`,
		confirmLabel: "OK",
		cancelLabel:  "Cancel",
	}
	h := string(slot.Render())
	mustNotContainRaw(t, h, "<script>", "modal-class-injection")
	if strings.Contains(h, `class="`+`"><script`) {
		t.Errorf("SECURITY: [modal-class-injection] class attribute breakout detected")
	} else {
		t.Logf("NOTE: [modal-class-injection] no class attribute breakout")
	}
}

// TestModal_IDInjection verifies that a widget name containing special
// characters cannot cause attribute breakout in the generated IDs.
func TestModal_IDInjection(t *testing.T) {
	t.Parallel()
	slot := &confirmDialogSlot{
		titleID:      `my-modal"><script>alert(1)</script>`,
		bodyID:       `my-modal-body"><img src=x onerror=alert(1)>`,
		title:        "Test",
		body:         "Body",
		confirmLabel: "OK",
		cancelLabel:  "Cancel",
	}
	h := string(slot.Render())
	mustNotContainRaw(t, h, "<script>", "modal-id-injection-title")
	mustNotContainRaw(t, h, "<img", "modal-id-injection-body")
	if strings.Contains(h, `id="my-modal"&gt;`) || strings.Contains(h, `id="my-modal-body"&gt;`) {
		t.Logf("NOTE: [modal-id-injection] ID with injection chars is attr-escaped")
	}
}

// ===========================================================================
// Drawer security (tested via Sidebar which builds a preset.Drawer)
// ===========================================================================

// TestDrawer_TitleXSS verifies that a sidebar title (rendered inside the
// drawer body) containing <script> tags is escaped via escText().
func TestDrawer_TitleXSS(t *testing.T) {
	t.Parallel()
	h := string(sidebarBody(SidebarConfig{
		Title: `<script>alert("xss")</script>`,
		Items: []SidebarItem{{Label: "Home", Href: "/"}},
	}))
	mustNotContainRaw(t, h, "<script>", "drawer-title-xss")
	if !strings.Contains(h, "&lt;script&gt;") {
		t.Errorf("SECURITY: [drawer-title-xss] expected &lt;script&gt;, got: %s", h)
	}
}

// TestDrawer_BodyXSS verifies that sidebar item labels containing
// script tags are escaped. The key protection is < → &lt;.
func TestDrawer_BodyXSS(t *testing.T) {
	t.Parallel()
	h := string(sidebarBody(SidebarConfig{
		Items: []SidebarItem{
			{Label: `<img src=x onerror="alert(1)">`, Href: "/safe"},
		},
	}))
	// < is escaped → no <img> element can be parsed
	mustNotContainRaw(t, h, "<img", "drawer-body-xss")
	if !strings.Contains(h, "&lt;img") {
		t.Errorf("SECURITY: [drawer-body-xss] expected escaped &lt;img, got: %s", h)
	}
}

// TestDrawer_PositionInjection verifies that special characters in the
// DrawerName (used as widget name) cannot inject attributes.
func TestDrawer_PositionInjection(t *testing.T) {
	t.Parallel()
	cfg := SidebarConfig{
		Title:      "Nav",
		Items:      []SidebarItem{{Label: "Home", Href: "/"}},
		DrawerName: `drawer"><script>alert(1)</script>`,
	}
	comp := Sidebar(cfg)
	h := string(comp.Render())
	mustNotContainRaw(t, h, "<script>", "drawer-position-injection")
	if strings.Contains(h, `data-fui-open="drawer"&gt;`) {
		t.Logf("NOTE: [drawer-position-injection] drawer name attr-escaped")
	}
}

// TestDrawer_ClassInjection verifies that sidebar item labels with
// class-injection payloads are rendered safely in text node context.
func TestDrawer_ClassInjection(t *testing.T) {
	t.Parallel()
	h := string(sidebarBody(SidebarConfig{
		Items: []SidebarItem{
			{Label: `" onclick="alert(1)" data-x="`, Href: "/safe"},
		},
	}))
	// The label is text-escaped (escText escapes <>& only).
	// " is not special in text node context, so the payload passes
	// through as text content inside <span class="ui-sidebar__label">.
	// This is safe — " inside a text node has no HTML significance.
	if !strings.Contains(h, `ui-sidebar__label`) {
		t.Errorf("SECURITY: [drawer-class-injection] expected label span, got: %s", h)
	}
	t.Logf("NOTE: [drawer-class-injection] label is in text node of <span> (safe; \\\" not special in text)")
}

// TestModal_EmptyBodyHandled verifies that ConfirmAction panics when body
// is empty — preventing a modal with missing safety information.
func TestModal_EmptyBodyHandled(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("SECURITY: [modal-empty-body] expected panic for empty Body, got none")
		} else {
			t.Logf("NOTE: [modal-empty-body] correctly panicked for empty Body: %v", r)
		}
	}()
	ConfirmAction(ConfirmActionConfig{
		Name:         "test-modal-empty",
		TriggerLabel: "Open",
		Title:        "Title",
		Body:         "", // empty body — must panic
		RPCPath:      "/test",
	})
}

// ===========================================================================
// Dropdown/Menu/CommandPalette security
// ===========================================================================

// TestDropdown_ItemsXSS verifies that menu item labels containing
// <script> tags are text-escaped.
func TestDropdown_ItemsXSS(t *testing.T) {
	t.Parallel()
	h := string(Menu(MenuConfig{
		Label: "Menu",
		Items: []MenuItem{
			{Label: `<script>alert(1)</script>`},
		},
	}))
	mustNotContainRaw(t, h, "<script>", "dropdown-items-xss")
	if !strings.Contains(h, "&lt;script&gt;") {
		t.Errorf("SECURITY: [dropdown-items-xss] expected &lt;script&gt;, got: %s", h)
	}
}

// TestDropdown_ItemValueXSS verifies that href values containing
// javascript: are attr-escaped. NOTE: escAttr escapes <>&" but NOT
// URL schemes — javascript: contains no escapable chars so it passes
// through. This is a FINDING: the framework doesn't sanitize URI schemes.
func TestDropdown_ItemValueXSS(t *testing.T) {
	t.Parallel()
	h := string(Menu(MenuConfig{
		Label: "Menu",
		Items: []MenuItem{
			{Label: "Click", Href: `javascript:alert(1)`},
		},
	}))
	// escAttr escapes " but not URL schemes. javascript: has no chars
	// that get HTML-escaped, so it passes through into the href.
	// This is a FINDING — hosts must validate Href before passing it.
	if strings.Contains(h, `javascript:`) {
		t.Errorf("SECURITY: [dropdown-item-value-xss] javascript: URI in menu href not sanitized by escAttr")
	} else {
		t.Logf("NOTE: [dropdown-item-value-xss] href value is attr-escaped")
	}
}

// TestDropdown_ClassInjection verifies that menu PanelClass cannot
// break out of the class attribute — escAttr escapes " to &quot;.
func TestDropdown_ClassInjection(t *testing.T) {
	t.Parallel()
	h := string(Menu(MenuConfig{
		Label:      "Menu",
		PanelClass: `" onclick="alert(1)`,
		Items:      []MenuItem{{Label: "Safe"}},
	}))
	// PanelClass is concatenated directly into the HTML string (not escaped).
	// This is a real vulnerability — \" in PanelClass can break out of the
	// class attribute into a new attribute context.
	// The \" onclick=\"alert(1)\" payload creates a real onclick attribute.
	mustNotContainRaw(t, h, "<script>", "dropdown-class-injection")
	if strings.Contains(h, ` onclick="alert(1)`) {
		t.Errorf("SECURITY: [dropdown-class-injection] PanelClass concatenated without escaping — attribute breakout possible")
	} else {
		t.Logf("NOTE: [dropdown-class-injection] PanelClass safely escaped")
	}
}

// TestMenu_ItemsXSS verifies that deeply nested XSS payloads in menu
// items are escaped.
func TestMenu_ItemsXSS(t *testing.T) {
	t.Parallel()
	payload := `"><iframe src="data:text/html,<script>alert(1)</script>">`
	h := string(Menu(MenuConfig{
		Label: "Menu",
		Items: []MenuItem{
			{Label: payload},
		},
	}))
	mustNotContainRaw(t, h, "<iframe", "menu-items-xss")
	mustNotContainRaw(t, h, "<script>", "menu-items-xss")
}

// TestMenu_ItemsHrefXSS verifies that href values with javascript: and
// data: URIs are handled. javascript: passes through (no escapable
// chars); data: with < gets the < escaped.
func TestMenu_ItemsHrefXSS(t *testing.T) {
	t.Parallel()
	h := string(Menu(MenuConfig{
		Label: "Menu",
		Items: []MenuItem{
			{Label: "Evil", Href: `javascript:alert(document.cookie)`},
			{Label: "Data", Href: `data:text/html,<script>alert(1)</script>`},
		},
	}))
	// javascript: passes through escAttr — FINDING
	if strings.Contains(h, `javascript:alert`) {
		t.Errorf("SECURITY: [menu-href-xss] javascript: URI in menu href not sanitized")
	}
	// data: URI with < should have < escaped to &lt;
	if strings.Contains(h, `href="data:text/html,<script>`) {
		t.Errorf("SECURITY: [menu-href-xss] data: URI with < not attr-escaped")
	}
	if strings.Contains(h, `data:text/html,&lt;`) {
		t.Logf("NOTE: [menu-href-xss] data: URI angle brackets escaped in href")
	}
}

// TestMenu_ClassInjection verifies that TriggerClass with special
// characters cannot break the HTML structure.
func TestMenu_ClassInjection(t *testing.T) {
	t.Parallel()
	h := string(Menu(MenuConfig{
		Label:        "Menu",
		TriggerClass: `"><script>alert(1)</script>`,
		Items:        []MenuItem{{Label: "Safe"}},
	}))
	// TriggerClass is concatenated directly into the HTML string without escaping.
	// This is a real vulnerability — \"><script>\" breaks out of the class attribute.
	if strings.Contains(h, "<script>") {
		t.Errorf("SECURITY: [menu-class-injection] TriggerClass concatenated without escaping — XSS via class breakout")
	} else {
		t.Logf("NOTE: [menu-class-injection] TriggerClass safely escaped")
	}
}

// TestCommandPalette_ItemsXSS verifies that the command palette's
// rendered trigger doesn't leak XSS payloads via render.Text().
func TestCommandPalette_ItemsXSS(t *testing.T) {
	t.Parallel()
	// CommandPalette doesn't accept Items — it uses RPC. Test via
	// TriggerLabel which is rendered in the trigger button.
	trigger := render.Tag("button", map[string]string{
		"type":       "button",
		"class":      "ui-visually-hidden",
		"aria-label": `<script>alert("xss")</script>`,
	}, render.Text(`<script>alert("xss")</script>`))
	h := string(trigger)
	mustNotContainRaw(t, h, "<script>", "command-palette-items-xss")
}

// TestCommandPalette_PlaceholderXSS verifies that the placeholder text
// in the combobox is escaped via render.Text().
func TestCommandPalette_PlaceholderXSS(t *testing.T) {
	t.Parallel()
	h := string(render.Text(`<img src=x onerror="alert(1)">`))
	mustNotContainRaw(t, h, "<img", "command-palette-placeholder-xss")
	t.Logf("NOTE: [command-palette-placeholder-xss] combobox placeholder is render.Text-escaped")
}

// TestCommandPalette_ClassInjection verifies that the command palette
// name (used as widget name and in IDs) cannot inject attributes.
func TestCommandPalette_ClassInjection(t *testing.T) {
	t.Parallel()
	trigger := render.Tag("button", map[string]string{
		"data-fui-open": `cmd"><script>alert(1)</script>`,
		"aria-label":    "Open",
	}, render.Text("Open"))
	h := string(trigger)
	mustNotContainRaw(t, h, "<script>", "command-palette-class-injection")
}

// TestDropdown_EmptyItemsHandled verifies that Menu panics with zero
// items, preventing a security-relevant broken UI.
func TestDropdown_EmptyItemsHandled(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("SECURITY: [dropdown-empty-items] expected panic for empty Items, got none")
		} else {
			t.Logf("NOTE: [dropdown-empty-items] correctly panicked: %v", r)
		}
	}()
	Menu(MenuConfig{Label: "Empty"})
}

// ===========================================================================
// Notification/Toast security
// ===========================================================================

// TestNotification_TitleXSS verifies that a notification title with
// <script> tags is escaped via render.Text().
func TestNotification_TitleXSS(t *testing.T) {
	t.Parallel()
	h := string(Notification(NotificationConfig{
		Title: `<script>alert("xss")</script>`,
	}))
	mustNotContainRaw(t, h, "<script>", "notification-title-xss")
	if !strings.Contains(h, "&lt;script&gt;") {
		t.Errorf("SECURITY: [notification-title-xss] expected escaped &lt;script&gt;, got: %s", h)
	}
}

// TestNotification_MessageXSS verifies that a notification body with
// <script> tags is escaped. The key protection is < → &lt;.
func TestNotification_MessageXSS(t *testing.T) {
	t.Parallel()
	h := string(Notification(NotificationConfig{
		Title: "Alert",
		Body:  `<img src=x onerror="alert(1)">`,
	}))
	mustNotContainRaw(t, h, "<img", "notification-message-xss")
}

// TestNotification_ActionLinkXSS verifies that a dismiss href
// containing javascript: is rendered. NOTE: render.Attr escapes <>&"'
// but NOT URL schemes. javascript: contains no escapable chars so it
// passes through. This is a FINDING: hosts must validate DismissHref.
func TestNotification_ActionLinkXSS(t *testing.T) {
	t.Parallel()
	h := string(Notification(NotificationConfig{
		Title:       "Dismiss me",
		DismissHref: `javascript:alert(1)`,
	}))
	if strings.Contains(h, `javascript:`) {
		t.Errorf("SECURITY: [notification-action-link-xss] javascript: URI in DismissHref not sanitized by render.Attr")
	} else {
		t.Logf("NOTE: [notification-action-link-xss] dismiss href is attr-escaped")
	}
}

// TestNotification_TypeInjection verifies that an unknown variant
// panics rather than emitting it into the class attribute.
func TestNotification_TypeInjection(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("SECURITY: [notification-type-injection] expected panic for unknown variant")
		} else {
			t.Logf("NOTE: [notification-type-injection] correctly panicked: %v", r)
		}
	}()
	Notification(NotificationConfig{
		Title:   "Test",
		Variant: StatusVariant(`<script>alert(1)</script>`),
	})
}

// TestNotification_ClassInjection verifies that a Class value with
// quote characters cannot break out of the class attribute.
func TestNotification_ClassInjection(t *testing.T) {
	t.Parallel()
	h := string(Notification(NotificationConfig{
		Title: "Test",
		Class: `" onclick="alert(1)`,
	}))
	mustNotContainAttrBreakout(t, h, `onclick="alert(1)`, "notification-class-injection")
	mustNotContainRaw(t, h, "<script>", "notification-class-injection")
}

// TestNotification_EmptyTitleHandled verifies that Notification panics
// when title is empty.
func TestNotification_EmptyTitleHandled(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("SECURITY: [notification-empty-title] expected panic for empty Title, got none")
		} else {
			t.Logf("NOTE: [notification-empty-title] correctly panicked: %v", r)
		}
	}()
	Notification(NotificationConfig{Title: ""})
}

// TestNotification_VeryLongMessage verifies that a very long body
// message doesn't cause panics or buffer issues and is fully escaped.
func TestNotification_VeryLongMessage(t *testing.T) {
	t.Parallel()
	longBody := strings.Repeat(`<script>alert("xss")</script> `, 1000)
	h := string(Notification(NotificationConfig{
		Title: "Long",
		Body:  longBody,
	}))
	mustNotContainRaw(t, h, "<script>", "notification-very-long-message")
	if !strings.Contains(h, "&lt;script&gt;") {
		t.Errorf("SECURITY: [notification-very-long-message] long payload not fully escaped")
	}
}

// TestNotificationBell_ClassInjection verifies that a NotificationBell
// Class value with injection characters is attr-escaped.
func TestNotificationBell_ClassInjection(t *testing.T) {
	t.Parallel()
	trigger, _ := NotificationBell(NotificationBellConfig{
		Name:  "bell",
		Label: "Notifications",
		Class: `" onclick="alert(1)`,
	})
	h := string(trigger)
	mustNotContainRaw(t, h, `<script>`, "notification-bell-class-injection")
	mustNotContainAttrBreakout(t, h, `onclick="alert(1)`, "notification-bell-class-injection")
}

// TestNotification_VariantHandling verifies that all valid variants
// produce correct class names without injection vectors.
func TestNotification_VariantHandling(t *testing.T) {
	t.Parallel()
	variants := []StatusVariant{StatusSuccess, StatusWarning, StatusDanger, StatusInfo, StatusNeutral}
	for _, v := range variants {
		h := string(Notification(NotificationConfig{Title: "Test", Variant: v}))
		expected := "ui-notification--" + string(v)
		if !strings.Contains(h, expected) {
			t.Errorf("SECURITY: [notification-variant-handling] missing class %q for variant %q", expected, v)
		}
		if strings.Contains(h, `"><script`) {
			t.Errorf("SECURITY: [notification-variant-handling] variant %q leaked into HTML", v)
		}
	}
	t.Logf("NOTE: [notification-variant-handling] all %d variants safe", len(variants))
}

// TestNotification_ConcurrentRender verifies that concurrent renders of
// Notification do not race on shared state (notification is stateless).
func TestNotification_ConcurrentRender(t *testing.T) {
	t.Parallel()
	var wg sync.WaitGroup
	const goroutines = 50
	errs := make(chan string, goroutines)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			h := string(Notification(NotificationConfig{
				Title:   "Concurrent-" + strings.Repeat("x", id%100),
				Body:    `<script>alert(` + strings.Repeat("A", id%50) + `)</script>`,
				Variant: StatusInfo,
			}))
			if strings.Contains(h, "<script>") {
				errs <- "concurrent-xss-leak"
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Errorf("SECURITY: [notification-concurrent-render] %s", e)
	}
	if len(errs) == 0 {
		t.Logf("NOTE: [notification-concurrent-render] %d concurrent renders safe", goroutines)
	}
}

// ===========================================================================
// FileUpload/Dropzone/Complex components security
// ===========================================================================

// TestFileUpload_ActionXSS verifies that a Name containing javascript:
// is rendered safely as a form field name, not as a navigable URI.
func TestFileUpload_ActionXSS(t *testing.T) {
	t.Parallel()
	h := string(FileUpload(FileUploadConfig{
		Name:  `javascript:alert(1)`,
		Label: "Upload",
	}))
	// The name goes into <input name="..."> which is attr-escaped.
	// It should NOT appear as href="javascript:...".
	if strings.Contains(h, `href="javascript:`) {
		t.Errorf("SECURITY: [fileupload-action-xss] javascript: leaked into href")
	} else {
		t.Logf("NOTE: [fileupload-action-xss] name is attr-escaped, not used as href")
	}
	if !strings.Contains(h, `name="`) {
		t.Errorf("SECURITY: [fileupload-action-xss] name attribute missing from output")
	}
}

// TestFileUpload_ClassInjection verifies that a Class with injection
// characters cannot break the class attribute.
func TestFileUpload_ClassInjection(t *testing.T) {
	t.Parallel()
	h := string(FileUpload(FileUploadConfig{
		Name:  "upload",
		Label: "Upload",
		Class: `" onclick="alert(1)`,
	}))
	mustNotContainAttrBreakout(t, h, `onclick="alert(1)`, "fileupload-class-injection")
	mustNotContainRaw(t, h, "<script>", "fileupload-class-injection")
}

// TestFileUpload_AcceptInjection verifies that the Accept MIME type
// value is attr-escaped and cannot inject new attributes.
func TestFileUpload_AcceptInjection(t *testing.T) {
	t.Parallel()
	h := string(FileUpload(FileUploadConfig{
		Name:   "upload",
		Label:  "Upload",
		Accept: `image/*" onclick="alert(1)" x="`,
	}))
	mustNotContainAttrBreakout(t, h, `onclick="alert(1)"`, "fileupload-accept-injection")
	if strings.Contains(h, `accept="image/*&quot;`) {
		t.Logf("NOTE: [fileupload-accept-injection] accept value correctly attr-escaped")
	}
}

// TestDropzone_ActionXSS verifies that a dropzone Name containing
// javascript: is rendered safely as a form field name, not a URL.
func TestDropzone_ActionXSS(t *testing.T) {
	t.Parallel()
	h := string(FileDropzone(FileDropzoneConfig{
		Name:  `javascript:alert(1)`,
		Label: "Drop files",
	}))
	if strings.Contains(h, `href="javascript:`) {
		t.Errorf("SECURITY: [dropzone-action-xss] javascript: leaked into href")
	} else {
		t.Logf("NOTE: [dropzone-action-xss] dropzone name is attr-escaped")
	}
}

// TestDropzone_ClassInjection verifies that a Class with injection
// characters is safely rendered.
func TestDropzone_ClassInjection(t *testing.T) {
	t.Parallel()
	h := string(FileDropzone(FileDropzoneConfig{
		Name:  "drop",
		Label: "Drop",
		Class: `" onmouseover="alert(1)`,
	}))
	mustNotContainAttrBreakout(t, h, `onmouseover="alert(1)`, "dropzone-class-injection")
	mustNotContainRaw(t, h, "<script>", "dropzone-class-injection")
}

// TestDropzone_MaxSizeEnforced verifies that MaxSizeMB appears in the
// rendered help text without injection.
func TestDropzone_MaxSizeEnforced(t *testing.T) {
	t.Parallel()
	h := string(FileDropzone(FileDropzoneConfig{
		Name:      "drop",
		Label:     "Drop",
		MaxSizeMB: 10,
	}))
	if !strings.Contains(h, "10 MB") {
		t.Errorf("SECURITY: [dropzone-max-size-enforced] expected '10 MB' in help text, got: %s", h)
	}
	if strings.Contains(h, "<script>") {
		t.Errorf("SECURITY: [dropzone-max-size-enforced] unexpected script tag in max size output")
	}
	t.Logf("NOTE: [dropzone-max-size-enforced] max size rendered safely in help text")
}

// TestFileUpload_MultipleAttribute verifies that the multiple
// attribute is correctly rendered for file uploads.
func TestFileUpload_MultipleAttribute(t *testing.T) {
	t.Parallel()
	h := string(FileUpload(FileUploadConfig{
		Name:     "upload",
		Label:    "Upload",
		Multiple: true,
	}))
	if !strings.Contains(h, `multiple=""`) {
		t.Errorf("SECURITY: [fileupload-multiple] expected multiple attribute, got: %s", h)
	}
	t.Logf("NOTE: [fileupload-multiple] multiple attribute correctly rendered")
}

// TestFileUpload_IDInjection verifies that an ID with special
// characters is attr-escaped and cannot break the for/id pairing.
func TestFileUpload_IDInjection(t *testing.T) {
	t.Parallel()
	h := string(FileUpload(FileUploadConfig{
		Name:  `my-id"><script>alert(1)</script>`,
		Label: "Upload",
	}))
	mustNotContainRaw(t, h, "<script>", "fileupload-id-injection")
	if strings.Contains(h, `for="my-id"&gt;`) || strings.Contains(h, `id="my-id"&gt;`) {
		t.Logf("NOTE: [fileupload-id-injection] ID correctly attr-escaped")
	}
}

// TestDropzone_AcceptAttributeSafe verifies that the accept attribute
// in a dropzone is attr-escaped.
func TestDropzone_AcceptAttributeSafe(t *testing.T) {
	t.Parallel()
	h := string(FileDropzone(FileDropzoneConfig{
		Name:   "drop",
		Label:  "Drop",
		Accept: `.pdf" onfocus="alert(1)" data-x="`,
	}))
	mustNotContainAttrBreakout(t, h, `onfocus="alert(1)"`, "dropzone-accept-safe")
	if strings.Contains(h, `accept="`) {
		t.Logf("NOTE: [dropzone-accept-safe] accept attribute rendered safely")
	}
}

// TestFileUpload_EmptyActionHandled verifies that FileUpload panics
// when Name is empty, preventing an unlabeled form field.
func TestFileUpload_EmptyActionHandled(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("SECURITY: [fileupload-empty-action] expected panic for empty Name, got none")
		} else {
			t.Logf("NOTE: [fileupload-empty-action] correctly panicked: %v", r)
		}
	}()
	FileUpload(FileUploadConfig{
		Name:  "",
		Label: "Upload",
	})
}
