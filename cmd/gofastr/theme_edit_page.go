// check-csp:ignore-file
//
// This file emits the local theme editor's inline bootstrap script. The
// editor's JS genuinely has nowhere else to live: it is per-session glue
// between the controls page and the preview iframe, and a separate asset
// pipeline for ~100 lines of bootstrap would cost more than it saved. The
// check-csp:ignore-file marker above is the CSP linter's exemption for that
// inline <script>; cmd/gofastr/harness_http.go carries the same marker for
// the same reason. Everything else in this file, the chrome's structural
// markup and its styling, composes framework/ui + core-ui components and
// links /__gofastr/app.css for the framework's default theme. No bespoke
// CSS ships here.
package main

import (
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// serveControlsPage renders the editor chrome: the token controls (left),
// the live preview iframe (right), the scheme toggle, the write button, and
// the contrast report. The page embeds the bearer token in a <meta> tag so
// the inline JS can authenticate POSTs without a query string.
func (s *themeEditServer) serveControlsPage(w http.ResponseWriter, r *http.Request) {
	// The WORKING theme, not the base one. Rendering the base meant a browser
	// refresh mid-session showed the original values in every swatch while the
	// server held the edited ones, and Write then emitted values the operator
	// could not see. The JSON endpoint always read `working`; only the HTML
	// render disagreed.
	controls := renderTokenControls(s.currentTokens())

	page := themeEditPageHTML(s.token, controls, s.outPath, s.previewThemeKey())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// The one page that carries the bearer token was the one page with no
	// defensive headers: everything else the tool serves inherits them from the
	// UIHost, and / and /__theme/* bypass it. Loopback binding and Host pinning
	// stop DNS rebinding; neither stops FRAMING, so any page the developer has
	// open could iframe this one and clickjack the Write button into
	// overwriting their theme file.
	// frame-ancestors and base-uri only. The page carries one inline
	// <script> (the editor's bootstrap glue; see the file header), so a
	// default-src policy would break the tool rather than protect it. Framing
	// is the threat that was actually open.
	w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'; base-uri 'none'")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(page))
}

// tokenGroupEntry is one category bucket of token controls.
type tokenGroupEntry struct {
	Name   string
	Order  int
	Tokens []tokenControl
}

type tokenControl struct {
	Key, Value, Type string
}

// renderTokenControls emits the grouped controls HTML from a flat token map.
// Tokens are grouped by key prefix (Colors, Spacing, …) with light/dark
// split, in a stable display order. Each group is a ui.Collapsible; each
// control is a ui-form-field-shaped row (see renderOneControl).
func renderTokenControls(tokens map[string]string) string {
	groups := groupTokenControls(tokens)
	parts := make([]render.HTML, 0, len(groups))
	for _, g := range groups {
		rows := make([]render.HTML, 0, len(g.Tokens))
		for _, t := range g.Tokens {
			rows = append(rows, renderOneControl(t))
		}
		// Open the two most-edited groups by default; collapse the rest.
		// Carrying the count in the summary text keeps the chrome clear of
		// bespoke badge markup: the count is what the operator scans for.
		parts = append(parts, ui.Collapsible(ui.CollapsibleConfig{
			Summary: fmt.Sprintf("%s (%d)", g.Name, len(g.Tokens)),
			Open:    g.Name == "Colors" || g.Name == "Colors (dark)",
		}, rows...))
	}
	return string(ui.Stack(ui.StackConfig{Gap: ui.GapSM}, parts...))
}

func groupTokenControls(tokens map[string]string) []tokenGroupEntry {
	groupMap := make(map[string]*tokenGroupEntry)
	order := map[string]int{
		"Colors":        0,
		"Colors (dark)": 1,
		"Spacing":       2,
		"Radii":         3,
		"Fonts":         4,
		"Typography":    5,
		"Shadows":       6,
		"Z-Index":       7,
		"Durations":     8,
		"Easings":       9,
		"Breakpoints":   10,
		"Code":          11,
		"Code (dark)":   12,
	}
	for k, v := range tokens {
		gn := tokenGroupName(k)
		ge, ok := groupMap[gn]
		if !ok {
			ge = &tokenGroupEntry{Name: gn, Order: 100}
			if o, ok := order[gn]; ok {
				ge.Order = o
			}
			groupMap[gn] = ge
		}
		ge.Tokens = append(ge.Tokens, tokenControl{Key: k, Value: v, Type: tokenControlType(k)})
	}
	out := make([]tokenGroupEntry, 0, len(groupMap))
	for _, g := range groupMap {
		sort.Slice(g.Tokens, func(i, j int) bool { return g.Tokens[i].Key < g.Tokens[j].Key })
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Order < out[j].Order })
	return out
}

func tokenGroupName(key string) string {
	isDark := strings.HasPrefix(key, "dark.")
	base := strings.TrimPrefix(key, "dark.")
	var group string
	switch {
	case strings.HasPrefix(base, "color-"):
		group = "Colors"
	case strings.HasPrefix(base, "tk-"):
		group = "Code"
	case strings.HasPrefix(base, "spacing-"):
		group = "Spacing"
	case strings.HasPrefix(base, "radii-"):
		group = "Radii"
	case strings.HasPrefix(base, "font-"):
		group = "Fonts"
	case strings.HasPrefix(base, "breakpoint-"):
		group = "Breakpoints"
	case strings.HasPrefix(base, "shadow-"):
		group = "Shadows"
	case strings.HasPrefix(base, "z-"):
		group = "Z-Index"
	case strings.HasPrefix(base, "duration-"):
		group = "Durations"
	case strings.HasPrefix(base, "easing-"):
		group = "Easings"
	case strings.HasPrefix(base, "text-"):
		group = "Typography"
	default:
		group = "Other"
	}
	if isDark {
		return group + " (dark)"
	}
	return group
}

// renderOneControl emits the labelled input(s) for a single token. Colour
// tokens get a colour-picker swatch alongside the text input (the text
// input is the source of truth; the picker is a convenience that writes
// hex back to it). Integer-px tokens strip the "px" suffix for display and
// the JS re-appends it on submit.
//
// Each row is a ui-form-field: the design system's stylesheet styles every
// descendant input/select/textarea via [data-fui-comp="ui-form-field"] input,
// so we get label spacing, focus rings, and the is-error affordance for free
// with no per-control CSS. The token key is carried on data-token (so the
// editor JS can find each control by key) and data-field on the wrapper (so
// the JS can flip is-error without selecting on a bespoke class).
func renderOneControl(t tokenControl) render.HTML {
	displayValue := t.Value
	if t.Type == "number-px" {
		displayValue = strings.TrimSuffix(t.Value, "px")
	}
	id := controlInputID(t.Key)

	var inputFrag render.HTML
	switch t.Type {
	case "color":
		// ui.ColorField is the design system's swatch + hex-input pair. Both
		// inputs carry data-token so the editor's JS wires them as one control;
		// the swatch writes its hex into the text input, which is the source of
		// truth (it can hold values the native picker cannot represent).
		inputFrag = ui.ColorField(ui.ColorFieldConfig{
			Value:       t.Value,
			SwatchValue: colorSwatchValue(t.Value),
			TextID:      id,
			SwatchLabel: t.Key + " colour swatch",
			// Matches the visible <label> text below, so the announced name and
			// the seen name are the same string.
			TextLabel: t.Key,
			SwatchAttrs: map[string]string{
				"data-token": t.Key,
				"data-type":  "color-swatch",
			},
			TextAttrs: map[string]string{
				"data-token": t.Key,
				"data-type":  t.Type,
			},
		})
	case "number", "number-px":
		inputFrag = render.VoidTag("input", map[string]string{
			"type":       "number",
			"value":      displayValue,
			"id":         id,
			"data-token": t.Key,
			"data-type":  t.Type,
		})
	default:
		inputFrag = render.VoidTag("input", map[string]string{
			"type":       "text",
			"value":      t.Value,
			"id":         id,
			"data-token": t.Key,
			"data-type":  t.Type,
		})
	}

	label := render.Tag("label", map[string]string{
		"class": "ui-form-field__label",
		"for":   id,
	}, render.Text(t.Key))

	// Empty error span: the JS fills this on apply failure. data-err-for is
	// the JS's lookup key; the design system's ui-form-field__error class
	// colours it via the --color-danger token.
	errSpan := render.Tag("span", map[string]string{
		"class":        "ui-form-field__error",
		"data-err-for": t.Key,
	})

	return render.Tag("div", map[string]string{
		"class":           "ui-form-field",
		"data-fui-comp":   "ui-form-field",
		"data-field":      t.Key,
		"data-field-type": t.Type,
	}, label, inputFrag, errSpan)
}

// controlInputID derives a stable, HTML-legal id from a token key. Used as
// the <input id> the label's for= points at; the design system's form-field
// stylesheet doesn't require it, but label association is a basic
// accessibility contract.
func controlInputID(key string) string {
	var b strings.Builder
	for _, r := range key {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return "te-input-" + b.String()
}

// colorSwatchValue returns a #rrggbb form the <input type="color"> can
// render, or "#000000" when the value is not a plain 3/4/6/8-digit hex (the
// picker cannot represent oklch/color-mix/var(), but the text input beside
// it always holds the true value).
func colorSwatchValue(v string) string {
	hex := strings.TrimPrefix(v, "#")
	switch len(hex) {
	case 3:
		// #rgb → #rrggbb
		r, g, b := hex[0], hex[1], hex[2]
		return "#" + string([]byte{r, r, g, g, b, b})
	case 6, 8:
		return "#" + hex[:6]
	}
	return "#000000"
}

// themeEditPageHTML composes the editor chrome from design-system primitives
// (ui.Stack, ui.Cluster, ui.Button, the ui-callout variant surface) plus the
// framework's ui.Workbench inspector shell. The chrome
// LINKS /__gofastr/app.css with no ?t= query, so it renders against the
// host app's DEFAULT theme, pinning the controls to known-good tokens even
// when the operator has set --color-text: transparent in the working theme.
// The working theme only lives in the preview iframe, which swaps to
// /__gofastr/app.css?t=<hash> via swapPreviewCSS.
//
// Every visual treatment comes from the design system: the structural
// The ui-* classes are framework-owned and styled by app.css; the
// chrome ships no bespoke stylesheet of its own. The inline <script> is the
// one exception and carries the check-csp:ignore-file exemption at the top
// of this file.
func themeEditPageHTML(token, controls, outPath, previewKey string) string {
	// Sidebar header: title + action buttons. ui.Cluster with justify-between
	// pushes the actions to the trailing edge; the title leads.
	title := render.Tag("h1", map[string]string{
		"class": "ui-pageheader__title",
	}, render.Text("theme edit"))
	schemeBtn := ui.Button(ui.ButtonConfig{
		Label:   "◐ Light",
		Variant: ui.ButtonSecondary,
		ID:      "te-scheme",
		Type:    "button",
	})
	writeBtn := ui.Button(ui.ButtonConfig{
		Label:   "Write",
		Variant: ui.ButtonPrimary,
		ID:      "te-write",
		Type:    "button",
	})
	headerRow := ui.Cluster(ui.ClusterConfig{
		Align:   ui.AlignCenter,
		Justify: ui.JustifyBetween,
	}, title, ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM}, schemeBtn, writeBtn))

	// Status line: a Callout the JS updates by swapping its variant class.
	// ui-callout--<success|danger|warning|info|neutral> recolours the leading
	// glyph via the --color-* tokens, so the chrome needs no per-status CSS.
	statusBox := render.Tag("div", map[string]string{
		"id":            "te-status",
		"role":          "status",
		"aria-live":     "polite",
		"class":         "ui-callout ui-callout--neutral",
		"data-fui-comp": "ui-callout",
	}, render.Tag("div", map[string]string{"class": "ui-callout__body"}, render.Text("")))

	// Contrast panel: JS fills this. Same Callout shape; the JS swaps to
	// ui-callout--warning when findings exist, hidden when none.
	contrastBox := render.Tag("div", map[string]string{
		"id":            "te-contrast",
		"role":          "alert",
		"hidden":        "",
		"class":         "ui-callout ui-callout--warning",
		"data-fui-comp": "ui-callout",
	})

	// The controls themselves are renderTokenControls output (ui.Collapsible
	// groups of ui-form-field rows). The wrapping div carries data-controls
	// so the JS can target it without selecting on a bespoke class.
	controlsBox := render.Tag("div", map[string]string{
		"data-controls": "",
	}, render.HTML(controls))

	sidebarInner := ui.Stack(ui.StackConfig{Gap: ui.GapMD},
		headerRow, statusBox, contrastBox, controlsBox)

	iframe := render.Tag("iframe", map[string]string{
		"id":    "te-frame",
		"src":   "/preview",
		"title": "Theme preview",
	})

	// ui.Workbench is the design system's two-pane inspector shell: a rail
	// that scrolls on its own beside a pane that fills the rest, with an
	// iframe in the pane filling it edge to edge. It was added for this: the
	// alternative was a rail with no scroll (a 2300px page) beside a preview
	// collapsed to the iframe's default ~300x150 box, which is what deleting
	// the old bespoke stylesheet without an upstream replacement produced.
	bodyInner := ui.Workbench(ui.WorkbenchConfig{
		Rail: sidebarInner,
		Pane: iframe,
		ExtraAttrs: map[string]string{
			"aria-label": "Theme editor",
		},
	})

	// The chrome renders OUTSIDE the UIHost render pipeline (it is a
	// hand-assembled page, not a registered screen), so the host never learns
	// which components it used and app.css carries none of their CSS. Scan the
	// rendered markup for component markers and link the host's own bundle
	// endpoint for exactly those, the same endpoint a screen's <link> uses.
	// Without this the chrome renders as unstyled HTML: no split, no scroll,
	// an iframe collapsed to its default box.
	used := registry.Scan(string(bodyInner))
	sort.Strings(used)
	head := []render.HTML{
		render.VoidTag("meta", map[string]string{"charset": "utf-8"}),
		render.VoidTag("meta", map[string]string{"name": "theme-edit-token", "content": token}),
		render.VoidTag("meta", map[string]string{"name": "theme-edit-out", "content": outPath}),
		render.VoidTag("meta", map[string]string{"name": "theme-edit-variant", "content": previewKey}),
		render.Tag("title", nil, render.Text("gofastr theme edit")),
		render.VoidTag("link", map[string]string{"rel": "stylesheet", "href": "/__gofastr/app.css"}),
	}
	if len(used) > 0 {
		head = append(head, render.VoidTag("link", map[string]string{
			"rel":  "stylesheet",
			"href": "/__gofastr/comp-bundle.css?names=" + url.QueryEscape(strings.Join(used, ",")),
		}))
	}

	page := render.Tag("html", map[string]string{"lang": "en", "data-color-scheme": "light"},
		render.Tag("head", nil, head...),
		render.Tag("body", nil,
			bodyInner,
			render.Tag("script", nil, render.HTML(themeEditChromeJS)),
		),
	)
	return "<!DOCTYPE html>\n" + string(page)
}

const themeEditChromeJS = `
(function() {
  var TOKEN = document.querySelector('meta[name="theme-edit-token"]').content;
  var OUT = document.querySelector('meta[name="theme-edit-out"]').content;
  var frame = document.getElementById('te-frame');
  var statusEl = document.getElementById('te-status');
  var contrastEl = document.getElementById('te-contrast');
  var schemeBtn = document.getElementById('te-scheme');
  var writeBtn = document.getElementById('te-write');
  var currentScheme = 'light';
  var pendingTimers = {};
  // pendingValues mirrors the value each pending debounce timer will
  // apply, so a Write that races the debounce can flush the in-flight
  // edit instead of emitting the theme without its last keystroke.
  var pendingValues = {};
  var applyQueue = Promise.resolve();
  // pendingError holds the most recent apply failure (validation or
  // transport). It is cleared by a successful apply and read by
  // flushPendingEdits: a racing Write must propagate the failure and
  // block /__theme/writeback, instead of resolving the queue as if all
  // was well and overwriting the file with the previous theme.
  var pendingError = null;

  function setStatus(msg, kind) {
    // Status lives inside a ui-callout. The variant class drives the colour
    // via the design system's --color-* tokens, so kind is mapped to a
    // Callout variant rather than a bespoke .te-status--<kind> rule.
    var variant = 'neutral';
    if (kind === 'ok') variant = 'success';
    else if (kind === 'err') variant = 'danger';
    var body = statusEl.querySelector('.ui-callout__body');
    if (body) body.textContent = msg || '';
    statusEl.className = 'ui-callout ui-callout--' + variant;
  }

  function authHeaders() {
    return { 'Authorization': 'Bearer ' + TOKEN, 'Content-Type': 'application/json' };
  }

  // Collect the server-bound value from a control input.
  function readControl(input) {
    var type = input.dataset.type;
    var val = input.value;
    if (type === 'number-px') return val + 'px';
    return val;
  }

  function findDataElement(selector, attr, key) {
    var nodes = document.querySelectorAll(selector);
    for (var i = 0; i < nodes.length; i++) {
      if (nodes[i].getAttribute(attr) === key) return nodes[i];
    }
    return null;
  }

  function findTextInput(key) {
    return findDataElement('[data-token]:not([data-type="color-swatch"])', 'data-token', key);
  }

  function showError(key, msg) {
    var input = findTextInput(key) || findDataElement('[data-token]', 'data-token', key);
    if (!input) return;
    // Flip the design system's is-error class on the wrapping form-field.
    // The variant CSS ([data-fui-comp="ui-form-field"].is-error input)
    // recolours the input border via --color-danger: no bespoke invalid
    // class needed.
    var field = input.closest('[data-field]');
    if (field) field.classList.add('is-error');
    var errEl = findDataElement('[data-err-for]', 'data-err-for', key);
    if (errEl) errEl.textContent = msg || 'invalid';
  }

  function clearError(key) {
    var input = findTextInput(key);
    if (!input) return;
    var field = input.closest('[data-field]');
    if (field) field.classList.remove('is-error');
    var errEl = findDataElement('[data-err-for]', 'data-err-for', key);
    if (errEl) errEl.textContent = '';
  }

  // Apply one token edit: POST /__theme/apply, swap the preview's app.css
  // link to the returned variant hash, re-run the contrast check. No iframe
  // reload: swapping the link href re-fetches only app.css and the browser
  // re-resolves every var() reference against the new :root values.
  function applyEdit(key, value) {
    return fetch('/__theme/apply', {
      method: 'POST',
      headers: authHeaders(),
      body: JSON.stringify({ key: key, value: value })
    }).then(function(r) { return r.json(); }).then(function(data) {
      if (data.error) {
        showError(key, data.error);
        // Name the offending token in the status. The server's error string
        // alone ("invalid color") doesn't identify the control, so the
        // operator would have to scan every field to find the bad one.
        var msg = key + ': ' + data.error;
        setStatus(msg, 'err');
        // REJECT so a racing Write (flushPendingEdits) sees the failure and
        // blocks /__theme/writeback. Resolving normally here, the previous
        // behaviour, let the Write handler overwrite the file with the
        // PREVIOUS theme while the control still showed the typed-but-
        // rejected value. The error carries themeValidation so the Write
        // handler knows applyEdit already set the status (don't overwrite).
        var err = new Error(msg);
        err.themeValidation = true;
        throw err;
      }
      // A successful apply clears any prior failure: the working theme on
      // the server now matches what the operator sees, so a subsequent Write
      // is safe to proceed.
      pendingError = null;
      clearError(key);
      setStatus('updated ' + key, 'ok');
      swapPreviewCSS(data.hash, function() { runContrastCheck(); });
    });
    // No .catch here: the rejection must propagate so queueApply can record
    // it in pendingError. The previous .catch swallowed every error, which
    // is why a failed apply certified the previous theme as if it had
    // landed.
  }

  function queueApply(key, value) {
    applyQueue = applyQueue.then(function() {
      return applyEdit(key, value);
    }).catch(function(e) {
      // Absorb the rejection into applyQueue so the NEXT keystroke's apply
      // can still fire: without this, one bad value would poison the queue
      // for the rest of the session. Record the failure in pendingError so
      // a racing Write sees it and blocks the writeback.
      if (e && e.themeValidation) {
        pendingError = e;
        return;
      }
      // Genuine transport error (fetch threw). Wrap so the Write handler's
      // status is honest about what went wrong.
      var wrapped = new Error('apply ' + key + ' failed: ' + e);
      wrapped.themeApply = true;
      setStatus(wrapped.message, 'err');
      pendingError = wrapped;
    });
  }

  // Swap the preview iframe's app.css link to the variant URL. The link is
  // the same /__gofastr/app.css path production uses; the ?t=<hash> selects
  // the RegisterThemeVariant content. Component CSS already loaded in the
  // iframe uses var(--*) refs, so it re-resolves without a re-fetch.
  // Swaps the preview stylesheet, then runs the callback once the NEW sheet
  // has actually applied.
  //
  // Swapping an href starts a fetch; computed styles keep reporting the old
  // sheet until it lands. Measuring immediately after the swap therefore
  // measured the PREVIOUS theme: so an edit that broke contrast was checked
  // against the values it replaced and reported clean. The retry loop could not
  // see it either: the old sheet is perfectly measurable, just wrong.
  // lastRequestedHash is the variant hash the most recent swapPreviewCSS
  // asked the iframe to load. appliedHash is the hash whose sheet has
  // actually applied (signalled by the link's load event). The check uses
  // the two together to tell which generation it measured: the readiness
  // sentinel only proves "a sheet applied" and reads identical
  // black-on-white for every theme, so without these the panel could not
  // tell a stale reading (the OLD theme still applied during a slow swap)
  // from a current one.
  var lastRequestedHash = null;
  var appliedHash = null;
  function swapPreviewCSS(hash, then) {
    var doc = frame.contentDocument;
    if (!doc) return;
    var link = doc.querySelector('link[href^="/__gofastr/app.css"]');
    if (!link) return;
    var href = '/__gofastr/app.css?t=' + encodeURIComponent(hash);
    lastRequestedHash = hash;
    if (link.getAttribute('href') === href) {
      // Already showing this exact sheet: record the generation and fire.
      appliedHash = hash;
      if (then) then();
      return;
    }
    if (then) {
      // Two paths fire the callback:
      //   1. the link's load event: the new sheet has landed and applied;
      //   2. a 1.5s fallback: covers cached sheets that apply in some
      //      browsers without dispatching load.
      //
      // The previous code locked out the load event after whichever fired
      // first (done = true). On a throttled connection the fallback fired
      // first, the late load was ignored, and the contrast check measured
      // the PREVIOUS theme and reported clean for the new one. The load
      // listener MUST stay armed past the fallback so a late load re-fires
      // the callback: and the load event is the only path that updates
      // appliedHash, so checkScheme can distinguish stale from current.
      var timer = setTimeout(function() {
        // Cached-sheet workaround. The new sheet may have applied without
        // a load event; fire the callback so the check runs. We deliberately do
        // NOT set appliedHash=hash here: on a slow network the new sheet
        // has not applied yet and stamping it would let checkScheme publish
        // a reading of the OLD sheet. If this is the cached-sheet case the
        // load event will fire too and stamp the hash; if it is the
        // slow-network case the late load stamps the hash when it lands.
        then();
      }, 1500);
      link.addEventListener('load', function() {
        clearTimeout(timer);
        appliedHash = hash;  // the load event is the source of truth
        then();  // always re-run: late loads must trigger a fresh check
      }, { once: true });
      link.addEventListener('error', function() {
        clearTimeout(timer);
        // Fetch failed: the OLD sheet is still applied. Don't update
        // appliedHash. Re-run the check so it doesn't stay frozen on a
        // reading from the previous swap.
        then();
      }, { once: true });
    }
    link.setAttribute('href', href);
  }

  // Debounced input handler: collects the edited token and sends it after a
  // short quiet period so rapid typing doesn't flood the server.
  function onControlInput(input) {
    var key = input.dataset.token;
    if (input.dataset.type === 'color-swatch') {
      // The colour picker writes its hex into the sibling text input, which
      // is the actual source of truth; let the text input's handler fire.
      var text = input.parentElement.querySelector('[data-token]:not([data-type="color-swatch"])');
      if (text) { text.value = input.value; key = text.dataset.token; input = text; }
    }
    var value = readControl(input);
    pendingValues[key] = value;
    clearTimeout(pendingTimers[key]);
    pendingTimers[key] = setTimeout(function() {
      queueApply(key, value);
      delete pendingTimers[key];
      delete pendingValues[key];
    }, 300);
  }

  // Flush every debounced edit and wait for the apply queue to drain. The Go
  // writeBack reads only s.working, so a Write that beat the 300 ms debounce
  // emitted the theme WITHOUT the operator's last keystroke: the value
  // typed into the control silently never reached the file. Flushing the
  // timers fires the queued applies, and awaiting applyQueue guarantees they
  // have landed in s.working before the writeback POST leaves the browser.
  //
  // If any apply in the flushed batch FAILED (validation or transport), the
  // returned promise rejects with pendingError. The Write handler must not
  // POST /__theme/writeback in that case: the working theme on the server
  // does not match what the operator sees, so writing it would silently
  // discard the value they just typed.
  function flushPendingEdits() {
    var keys = Object.keys(pendingTimers);
    for (var i = 0; i < keys.length; i++) {
      var k = keys[i];
      var id = pendingTimers[k];
      if (!id) { delete pendingTimers[k]; continue; }
      clearTimeout(id);
      delete pendingTimers[k];
      if (pendingValues[k] !== undefined) {
        queueApply(k, pendingValues[k]);
        delete pendingValues[k];
      }
    }
    return applyQueue.then(function() {
      // applyQueue always resolves (queueApply absorbs rejections into
      // pendingError). Surface the failure here so the Write handler's
      // .catch fires instead of its .then → /__theme/writeback.
      if (pendingError) throw pendingError;
    });
  }

  // ---- Scheme toggle ----------------------------------------------------
  function setScheme(scheme) {
    currentScheme = scheme;
    schemeBtn.textContent = scheme === 'dark' ? '◑ Dark' : '◐ Light';
    var doc = frame.contentDocument;
    if (doc && doc.documentElement) {
      doc.documentElement.setAttribute('data-color-scheme', scheme);
    }
    runContrastCheck();
  }

  schemeBtn.addEventListener('click', function() {
    setScheme(currentScheme === 'dark' ? 'light' : 'dark');
  });

  // ---- Write-back -------------------------------------------------------
  writeBtn.addEventListener('click', function() {
    writeBtn.disabled = true;
    setStatus('writing ' + OUT + ' …');
    flushPendingEdits().then(function() {
      return fetch('/__theme/writeback', { method: 'POST', headers: authHeaders(), body: '{}' });
    })
      .then(function(r) { return r.json(); })
      .then(function(data) {
        writeBtn.disabled = false;
        if (data.error) { setStatus(data.error, 'err'); return; }
        setStatus('wrote ' + (data.path || OUT), 'ok');
      })
      .catch(function(e) {
        writeBtn.disabled = false;
        // A validation/apply failure already named the offending token in
        // setStatus (applyEdit or queueApply wrote the message). Overwriting
        // it with "network error" would hide which token was invalid.
        if (e && (e.themeValidation || e.themeApply)) return;
        setStatus('write blocked: ' + e, 'err');
      });
  });

  // ---- Contrast check (browser-side, every colour space) ----------------
  // getComputedStyle resolves oklch(), color-mix(), var() etc. natively;
  // the repo's only Go-side contrast helper is hex-only and returns 0 for
  // the oklch values real themes use. We check BOTH schemes by flipping the
  // iframe's data-color-scheme and reading the probe elements.
  // Converts ANY CSS colour the browser understands into sRGB bytes.
  //
  // Hand-parsing three numbers out of the computed value is wrong twice over.
  // getComputedStyle does not promise rgb(): Chromium hands back oklch(...)
  // and oklab(...) verbatim: so "oklch(1 0 0)" (white) parsed as [1,0,0]
  // (nearly black) and a 21:1 pair was reported as failing at 1.00:1. And the
  // theme's own values ARE oklch, which is the whole reason this check runs in
  // a browser instead of in Go.
  //
  // Painting one pixel delegates colour-space parsing to the browser. Two
  // fillStyle sentinels distinguish an invalid assignment from every valid
  // spelling of black.
  var _cvx = null;
  function toRGB(color) {
    var v = String(color).trim();
    if (!v) return null;
    if (!_cvx) {
      var c = document.createElement('canvas');
      c.width = c.height = 1;
      _cvx = c.getContext('2d', { willReadFrequently: true });
    }
    if (!_cvx) return null;
    _cvx.fillStyle = '#010203';
    _cvx.fillStyle = v;
    var first = _cvx.fillStyle;
    _cvx.fillStyle = '#040506';
    _cvx.fillStyle = v;
    if (_cvx.fillStyle !== first) return null;

    _cvx.clearRect(0, 0, 1, 1);
    _cvx.fillStyle = v;
    _cvx.fillRect(0, 0, 1, 1);
    var d = _cvx.getImageData(0, 0, 1, 1).data;
    return [d[0], d[1], d[2], d[3] / 255];
  }
  function parseRgb(color) { return toRGB(color); }

  // Porter-Duff source-over compositing. Both inputs are [r,g,b,a].
  function composite(top, bottom) {
    var alpha = top[3] + bottom[3] * (1 - top[3]);
    if (alpha === 0) return [0, 0, 0, 0];
    var scale = bottom[3] * (1 - top[3]);
    return [
      (top[0] * top[3] + bottom[0] * scale) / alpha,
      (top[1] * top[3] + bottom[1] * scale) / alpha,
      (top[2] * top[3] + bottom[2] * scale) / alpha,
      alpha
    ];
  }

  // WCAG relative luminance: sRGB channel to linear, then the standard weights.
  function channel(v) {
    var c = v / 255;
    return c <= 0.03928 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4);
  }
  function luminance(rgb) { return 0.2126 * channel(rgb[0]) + 0.7152 * channel(rgb[1]) + 0.0722 * channel(rgb[2]); }
  function ratio(fg, bg, behind) {
    var a = parseRgb(fg), b = parseRgb(bg);
    if (!a || !b) return null;
    var renderedBg = composite(b, behind || [255, 255, 255, 1]);
    var renderedFg = composite(a, renderedBg);
    var l1 = luminance(renderedFg), l2 = luminance(renderedBg);
    var hi = Math.max(l1, l2), lo = Math.min(l1, l2);
    return (hi + 0.05) / (lo + 0.05);
  }
  // Returns null when the stylesheet has not applied yet, so the caller waits
  // instead of publishing a reading it does not have.
  //
  // Readiness is decided by ONE sentinel probe whose colours are literals, not
  // var() references. A pair that resolves transparent is a real answer (the
  // element inherits the page background), not evidence of a missing sheet:
  // treating it as the latter aborted the whole check whenever any scheme had
  // a transparent background, and the panel then kept whatever it last showed.
  function checkScheme(scheme) {
    var doc = frame.contentDocument;
    if (!doc) return null;
    doc.documentElement.setAttribute('data-color-scheme', scheme);
    var probes = doc.querySelectorAll('[data-cp]');
    if (!probes.length) return null;

    // Verify the iframe is showing the generation we asked for. The
    // readiness sentinel below only proves "some sheet applied": it reads
    // literal black-on-white under BOTH the old and new themes, so without
    // this guard the panel measured the PREVIOUS theme's already-applied
    // sheet during a slow swap and certified the old values as the new
    // ones. lastRequestedHash is set the moment swapPreviewCSS starts the
    // fetch; appliedHash is set only when the link's load event fires, so a
    // mismatch means the new sheet is still in flight.
    if (lastRequestedHash && appliedHash !== lastRequestedHash) return null;


    var win = frame.contentWindow;
    var sentinel = doc.querySelector('[data-cp="ready|sentinel"]');
    if (!sentinel) return null;
    var sc = win.getComputedStyle(sentinel);
    // The sentinel declares literal black on literal white with no var()
    // references, so once the generated stylesheet has applied it MUST read
    // back as exactly that. Testing for a non-null ratio tested nothing:
    // getComputedStyle always yields a parseable colour, so an unstyled
    // document measured as inherited black over transparent, composited to
    // ~21:1, and the panel reported a clean bill of health for a theme it had
    // never measured. It also made the retry loop below unreachable.
    if (sc.color !== 'rgb(0, 0, 0)' || sc.backgroundColor !== 'rgb(255, 255, 255)') return null;

    // The browser's canvas is transparent. Resolve the root over white, then
    // the body over that root. Probe backgrounds composite over this measured
    // page background; foregrounds composite over the resulting probe fill.
    var transparent = [0, 0, 0, 0];
    var white = [255, 255, 255, 1];
    var rootBg = parseRgb(win.getComputedStyle(doc.documentElement).backgroundColor) || transparent;
    var bodyBg = parseRgb(win.getComputedStyle(doc.body).backgroundColor) || transparent;
    var pageBg = composite(bodyBg, composite(rootBg, white));

    var fails = [];
    for (var i = 0; i < probes.length; i++) {
      var p = probes[i];
      var name = p.getAttribute('data-cp');
      if (name === 'ready|sentinel') continue;
      var cs = win.getComputedStyle(p);
      var bg = cs.backgroundColor;
      var r = ratio(cs.color, bg, pageBg);
      if (r === null) {
        // Report it. Dropping a pair the browser served in a form we could not
        // convert is how a checker quietly shrinks to the subset it happens to
        // understand and still says "all clear".
        fails.push({ name: name, scheme: scheme, ratio: null, why: cs.color + ' on ' + bg });
        continue;
      }
      if (r < 4.5) fails.push({ name: name, scheme: scheme, ratio: r });
    }
    return fails;
  }
  // Retries until the probes are actually measurable rather than trusting a
  // fixed delay. The stylesheet the probes depend on is fetched by the frame,
  // and a swap re-fetches it; measuring on a timer meant sometimes measuring an
  // unstyled document and publishing the result as fact.
  function runContrastCheck(attempt) {
    try {
      runContrastCheckInner(attempt);
    } catch (e) {
      // Never fail silently. A hidden panel reads as "no contrast problems",
      // so an exception in here does not just lose the check: it asserts a
      // clean bill of health. Say what happened instead.
      renderContrastError(String((e && e.message) || e));
    }
  }
  function runContrastCheckInner(attempt) {
    attempt = attempt || 0;
    var doc = frame.contentDocument;
    if (!doc) return;
    var light = checkScheme('light');
    var dark = light === null ? null : checkScheme('dark');
    if (light === null || dark === null) {
      doc.documentElement.setAttribute('data-color-scheme', currentScheme);
      if (attempt < 20) setTimeout(function() { runContrastCheck(attempt + 1); }, 100);
      else renderContrastError('probes never became measurable');
      return;
    }
    var fails = light.concat(dark);
    // Restore the user's selected scheme.
    doc.documentElement.setAttribute('data-color-scheme', currentScheme);
    renderContrast(fails);
  }
  function renderContrast(fails) {
    if (fails.length === 0) {
      contrastEl.hidden = true;
      contrastEl.innerHTML = '';
      return;
    }
    contrastEl.hidden = false;
    var items = fails.map(function(f) {
      if (f.ratio === null) {
        return '<li>' + f.scheme + ': ' + htmlEsc(f.name) + ' — not measurable (' + htmlEsc(f.why || '') + ')</li>';
      }
      return '<li>' + f.scheme + ': ' + htmlEsc(f.name) + ' — ' + f.ratio.toFixed(2) + ':1</li>';
    });
    contrastEl.innerHTML = '<h4>Contrast findings (' + fails.length + ')</h4><ul>' + items.join('') + '</ul>';
  }
  // A broken check must LOOK broken. Reusing the hidden state for "nothing to
  // report" and for "the check did not run" is what let this surface report a
  // clean bill of health for every theme it was ever pointed at.
  function renderContrastError(msg) {
    contrastEl.hidden = false;
    contrastEl.innerHTML = '<h4>Contrast check unavailable</h4><ul><li>' + htmlEsc(msg) + '</li></ul>';
  }
  function htmlEsc(s) { return s.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;'); }

  // ---- Wire up controls -------------------------------------------------
  document.querySelectorAll('[data-token]').forEach(function(el) {
    el.addEventListener('input', function() { onControlInput(el); });
  });

  // Run the contrast check once the iframe has loaded its first paint.
  // Adopt the working theme when the frame loads.
  //
  // The preview page is server-rendered with the app theme; the working theme
  // lives in a registered variant. Without this a reload showed edited values in
  // every control beside a preview that had silently reverted: the same
  // stale-state bug as rendering the controls from the base theme, one level
  // out.
  frame.addEventListener('load', function() {
    var v = document.querySelector('meta[name="theme-edit-variant"]');
    var key = v ? v.getAttribute('content') : '';
    if (key) swapPreviewCSS(key, function() { runContrastCheck(); });
    else runContrastCheck();
  });
})();
`
