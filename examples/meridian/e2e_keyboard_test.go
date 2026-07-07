package main

// Keyboard-only traversal gate for the Meridian flagship — the WCAG chunk
// axe cannot automate. Mirrors examples/site/e2e_keyboard_test.go's probe
// (the two apps are separate packages, so the JS + Go helpers are duplicated
// rather than shared) and adds the quick-add MODAL trap-then-release gate.
//
// For each page we drive ONLY the keyboard (chromedp.KeyEvent with kb.Tab /
// kb.Enter / kb.Escape — never a synthetic focus() to advance the walk) and
// assert:
//
//  a) TERMINATION / NO TRAP: repeated Tab from <body> cycles back to the
//     first tabbable within a sane bound.
//  b) VISIBILITY: every focused element is actually visible (rect intersects
//     the viewport AFTER focus, so browsers' scroll-on-focus makes an
//     off-screen control pass).
//  c) FOCUS INDICATION PAINTS: outline != none (width>0), OR a non-trivial
//     box-shadow, OR a border/background change vs the blurred snapshot.
//  d) COMPLETENESS: every rendered interactive element is reachable.
//
// MODAL gate (TestModalFocusTrap): Enter opens the quick-add modal from its
// trigger; Tab CYCLES WITHIN the modal only (focus never leaves
// [data-fui-widget="customer-quick-add"]); Escape closes it; focus RETURNS to
// the trigger. The modal's own focusables also pass the indicator gate.
//
// Real defects are fixed upstream in framework/ui via the token/variant
// system; the failing gate IS the failing test.

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// kbgateSetupJS installs window.__kbgate — see the examples/site counterpart.
const kbgateSetupJS = `(() => {
  const NS = window.__kbgate = window.__kbgate || {};
  const TAB_SEL = 'a[href], area[href], button:not([disabled]):not([aria-disabled="true"]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), summary, [tabindex]:not([tabindex="-1"]), iframe, audio[controls], video[controls]';
  const HIDDEN_VIS = { 'hidden': true, 'collapse': true };

  NS._snap = function(el) {
    const cs = getComputedStyle(el);
    return {
      outlineStyle: cs.outlineStyle, outlineWidth: parseFloat(cs.outlineWidth) || 0,
      boxShadow: cs.boxShadow, borderTopColor: cs.borderTopColor,
      borderTopWidth: parseFloat(cs.borderTopWidth) || 0,
      backgroundColor: cs.backgroundColor, textDecoration: cs.textDecorationLine || cs.textDecoration || ''
    };
  };

  // ─── Perceptible focus-indicator helpers ─────────────────────────
  // The old detection accepted imperceptible deltas: outline-width
  // 0.01px passed ">0"; a fully-transparent zero-spread box-shadow
  // passed "!== 'none'"; a 1%-alpha background tweak passed string
  // inequality. These helpers enforce thresholds a human eye can see.

  // _parseColor converts a CSS color string to {r,g,b,a}. Handles
  // rgb()/rgba(), hex, transparent, and oklch()/oklab() — the modern
  // color formats this theme defines its tokens in. Chrome preserves
  // oklch() verbatim in computed values (it does NOT convert to rgb),
  // so a regex limited to rgb()/rgba() is blind to the entire theme.
  // oklch/oklab are converted to sRGB via the standard OKLab matrix.
  NS._oklabToRGB = function(L, a, b) {
    const l_ = L + 0.3963377774*a + 0.2158037573*b;
    const m_ = L - 0.1055613458*a - 0.0638541728*b;
    const s_ = L - 0.0894841775*a - 1.2914855480*b;
    const l = l_*l_*l_, mm = m_*m_*m_, ss = s_*s_*s_;
    const gam = function(c) { return c <= 0.0031308 ? 12.92*c : 1.055*Math.pow(Math.max(0,c), 1/2.4) - 0.055; };
    return {
      r: Math.round(Math.min(255, Math.max(0, gam(4.0767416621*l - 3.3077115913*mm + 0.2309699292*ss) * 255))),
      g: Math.round(Math.min(255, Math.max(0, gam(-1.2684380046*l + 2.6097574011*mm - 0.3413193965*ss) * 255))),
      b: Math.round(Math.min(255, Math.max(0, gam(-0.0041960863*l - 0.7034186147*mm + 1.7076147010*ss) * 255)))
    };
  };
  NS._parseColor = function(s) {
    if (!s || typeof s !== 'string') return null;
    s = s.trim();
    if (!s || s === 'none') return null;
    if (s === 'transparent') return { r: 0, g: 0, b: 0, a: 0 };
    let m = s.match(/rgba?\(\s*([\d.]+)\s*,?\s*([\d.]+)\s*,?\s*([\d.]+)\s*(?:[,/]\s*([\d.]+)\s*)?\)/i);
    if (m) return { r: +m[1], g: +m[2], b: +m[3], a: m[4] !== undefined ? +m[4] : 1 };
    m = s.match(/^#([0-9a-f]{6})([0-9a-f]{2})?$/i);
    if (m) return { r: parseInt(m[1].slice(0,2),16), g: parseInt(m[1].slice(2,4),16), b: parseInt(m[1].slice(4,6),16), a: m[2] ? parseInt(m[2],16)/255 : 1 };
    // oklch(L C H [ / alpha]) — space-separated, H in degrees
    m = s.match(/oklch\(\s*([\d.]+)(?:deg)?\s+([\d.]+)\s+([\d.]+)(?:deg)?\s*(?:\/\s*([\d.%]+)\s*)?\)/i);
    if (m) {
      const hr = +m[3] * Math.PI / 180;
      const rgb = NS._oklabToRGB(+m[1], +m[2]*Math.cos(hr), +m[2]*Math.sin(hr));
      rgb.a = m[4] !== undefined ? (m[4].endsWith('%') ? +m[4].slice(0,-1)/100 : +m[4]) : 1;
      return rgb;
    }
    // oklab(L a b [ / alpha])
    m = s.match(/oklab\(\s*([\d.-]+)\s+([\d.-]+)\s+([\d.-]+)\s*(?:\/\s*([\d.%]+)\s*)?\)/i);
    if (m) {
      const rgb = NS._oklabToRGB(+m[1], +m[2], +m[3]);
      rgb.a = m[4] !== undefined ? (m[4].endsWith('%') ? +m[4].slice(0,-1)/100 : +m[4]) : 1;
      return rgb;
    }
    return null;
  };

  // _colorDelta reports whether two colors differ perceptibly: an alpha
  // delta > 0.1 OR an RGB Euclidean distance > 20. Finer deltas are
  // imperceptible to the eye.
  NS._colorDelta = function(a, b) {
    const ca = NS._parseColor(a), cb = NS._parseColor(b);
    if (!ca || !cb) return a !== b; // unparseable → conservative string compare
    if (Math.abs(ca.a - cb.a) > 0.1) return true;
    const dr = ca.r - cb.r, dg = ca.g - cb.g, db = ca.b - cb.b;
    return Math.sqrt(dr*dr + dg*dg + db*db) > 20;
  };

  // _shadowIsVisible reports whether a box-shadow has at least one
  // layer with (blur+spread) > 0 AND a color alpha > 0.1. A non-"none"
  // shadow can still be imperceptible (zero blur+spread, or fully
  // transparent color). Colors in any format (oklch, hsl, rgb, hex) are
  // masked before splitting so their internal commas don't split layers.
  NS._shadowIsVisible = function(val) {
    if (!val || val === 'none') return false;
    const colorRe = /(?:oklch|oklab|hsla?|rgba?)\([^)]*\)|#[0-9a-fA-F]{3,8}/gi;
    const colors = val.match(colorRe) || [];
    const layers = val.replace(colorRe, '').split(',');
    for (let i = 0; i < layers.length; i++) {
      const nums = layers[i].match(/-?[\d.]+/g) || [];
      let blur = 0, spread = 0;
      if (nums.length >= 3) blur = parseFloat(nums[2]) || 0;
      if (nums.length >= 4) spread = parseFloat(nums[3]) || 0;
      if ((blur + spread) <= 0) continue;
      const color = (i < colors.length) ? NS._parseColor(colors[i]) : null;
      if (color && color.a > 0.1) return true;
    }
    return false;
  };

  // _hasIndicator judges whether a focused element shows a perceptible
  // focus indicator, returning '' (none) or a reason string.
  NS._hasIndicator = function(focused, blurred) {
    if (focused.outlineStyle !== 'none' && focused.outlineWidth >= 1) {
      return 'outline ' + focused.outlineWidth + 'px';
    }
    if (NS._shadowIsVisible(focused.boxShadow)) return 'box-shadow';
    if (blurred) {
      if (NS._colorDelta(focused.backgroundColor, blurred.backgroundColor)) return 'bg change';
      if (NS._colorDelta(focused.borderTopColor, blurred.borderTopColor)) return 'border-color change';
      if (Math.abs(focused.borderTopWidth - blurred.borderTopWidth) > 0.1) return 'border-width change';
      if (focused.textDecoration !== blurred.textDecoration) return 'text-decoration change';
    }
    return '';
  };

  NS._sig = function(el) {
    const parts = []; let cur = el, depth = 0;
    while (cur && cur.nodeType === 1 && depth < 6) {
      let part = cur.tagName.toLowerCase();
      if (cur.id) part += '#' + cur.id;
      const cls = (cur.getAttribute('class') || '').trim().split(/\s+/).filter(Boolean).slice(0, 2).join('.');
      if (cls) part += '.' + cls;
      if (cur.dataset && cur.dataset.fuiComp) part += '[fui=' + cur.dataset.fuiComp + ']';
      if (cur.dataset && cur.dataset.fuiOpen) part += '[open=' + cur.dataset.fuiOpen + ']';
      parts.unshift(part); cur = cur.parentElement; depth++;
    }
    return parts.join('>') || el.tagName.toLowerCase();
  };

  // isRendered: true only for elements that paint a box in the current layout.
  // An element's OWN computed display stays non-"none" even when an ancestor
  // is display:none (a closed <details> / a desktop-hidden mobile nav), so we
  // cannot filter on display alone — checkVisibility() reflects real rendering.
  NS._isRendered = function(el) {
    if (!el || !el.isConnected) return false;
    if (typeof el.checkVisibility === 'function') return el.checkVisibility();
    const cs = getComputedStyle(el);
    if (cs.position === 'fixed') { const r = el.getBoundingClientRect(); return r.width > 0 || r.height > 0; }
    return el.offsetParent !== null;
  };

  NS.reset = function() {
    document.querySelectorAll('[data-kbgate-i],[data-kbgate-m]').forEach((el) => {
      el.removeAttribute('data-kbgate-i'); el.removeAttribute('data-kbgate-m');
    });
    NS.tabbables = []; NS.visits = []; NS._next = 0;
    NS._modalRoot = null; NS._modalItems = []; NS._modalVisits = []; NS._modalNext = 0;
  };

  NS.enumerate = function() {
    NS.reset();
    if (document.activeElement && document.activeElement !== document.body && document.activeElement.blur) {
      document.activeElement.blur();
    }
    if (document.body && document.body.focus) document.body.focus({ preventScroll: true });
    const items = [];
    // Radio groups: the browser tabs to ONE radio per name (the checked
    // one, else the first in DOM order). Non-representative radios are
    // correctly skipped by the tab order, so we must NOT enumerate them or
    // the completeness gate reports false "never reached" failures.
    const radioRep = {}; // name -> representative element
    const all = Array.from(document.querySelectorAll(TAB_SEL));
    all.forEach((el) => {
      if (el.tagName === 'INPUT' && el.type === 'radio' && el.name) {
        if (!(el.name in radioRep)) radioRep[el.name] = el; // first wins
        if (el.checked) radioRep[el.name] = el;             // checked overrides
      }
    });
    all.forEach((el) => {
      if (!NS._isRendered(el)) return;
      const cs = getComputedStyle(el);
      if (HIDDEN_VIS[cs.visibility]) return;
      if (el.tagName === 'INPUT' && el.type === 'radio' && el.name && radioRep[el.name] !== el) return;
      el.setAttribute('data-kbgate-i', String(items.length));
      items.push({ i: items.length, tag: el.tagName.toLowerCase(), id: el.id || '',
        cls: (el.getAttribute('class') || '').slice(0, 90), sel: NS._sig(el), blurred: NS._snap(el) });
    });
    NS.tabbables = items; NS._next = items.length; NS.visits = [];
    return { count: items.length, items: items };
  };

  NS.recordFocus = function() {
    const el = document.activeElement;
    const out = { idx: -1, cycled: false, visible: false, inViewport: false,
      w: 0, h: 0, hasIndicator: false, indicatorReason: '', newEl: false,
      tag: '', id: '', cls: '', sig: '' };
    if (!el || el === document.body || el === document.documentElement) {
      out.cycled = NS.visits.length > 0;
      out.sig = !el ? 'none' : (el === document.body ? 'body' : 'html');
      NS.visits.push(-1); return out;
    }
    let a = el.getAttribute('data-kbgate-i');
    if (a === null || a === '') { a = String(NS._next++); el.setAttribute('data-kbgate-i', a); out.newEl = true; }
    const idx = parseInt(a, 10);
    out.idx = idx; out.tag = el.tagName.toLowerCase(); out.id = el.id || '';
    out.cls = (el.getAttribute('class') || '').slice(0, 90); out.sig = NS._sig(el);
    const focused = NS._snap(el);
    const blurred = (!out.newEl && NS.tabbables[idx]) ? NS.tabbables[idx].blurred : null;
    const reason = NS._hasIndicator(focused, blurred);
    out.hasIndicator = reason !== ''; out.indicatorReason = reason;
    const r = el.getBoundingClientRect(); const cs = getComputedStyle(el);
    out.w = Math.round(r.width); out.h = Math.round(r.height);
    out.inViewport = r.bottom > 0 && r.right > 0 && r.top < (window.innerHeight || document.documentElement.clientHeight) &&
      r.left < (window.innerWidth || document.documentElement.clientWidth);
    out.visible = cs.display !== 'none' && !HIDDEN_VIS[cs.visibility] && r.width > 0 && r.height > 0 && out.inViewport;
    if (NS.visits.length > 0 && idx === NS.visits[0]) out.cycled = true;
    NS.visits.push(idx);
    return out;
  };

  // ─── Modal-scoped probe ────────────────────────────────────────────
  // enumerateIn tags only the focusables inside rootSel (the modal widget),
  // blurs the active element first so the first element's snapshot is blurred.
  const MODAL_TAB = 'a[href], button:not([disabled]):not([aria-disabled="true"]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';
  NS.enumerateIn = function(rootSel) {
    const root = document.querySelector(rootSel);
    if (!root) return { count: 0, error: 'root not found' };
    NS._modalRoot = root; NS._modalItems = []; NS._modalVisits = []; NS._modalNext = 0;
    if (document.activeElement && document.activeElement !== document.body && document.activeElement.blur) document.activeElement.blur();
    const items = [];
    root.querySelectorAll(MODAL_TAB).forEach((el) => {
      if (!NS._isRendered(el)) return;
      const cs = getComputedStyle(el);
      if (HIDDEN_VIS[cs.visibility]) return;
      el.setAttribute('data-kbgate-m', String(items.length));
      items.push({ i: items.length, tag: el.tagName.toLowerCase(), id: el.id || '',
        cls: (el.getAttribute('class') || '').slice(0, 80), sel: NS._sig(el), blurred: NS._snap(el) });
    });
    NS._modalItems = items; NS._modalNext = items.length;
    return { count: items.length, items: items };
  };

  // recordModalFocus: reports whether focus is contained in the modal, the
  // indicator verdict, and a cycle (focus returned to the first modal focusable).
  NS.recordModalFocus = function() {
    const el = document.activeElement;
    const contained = !!(el && NS._modalRoot && NS._modalRoot.contains(el));
    const out = { contained: contained, idx: -1, cycled: false, visible: false,
      hasIndicator: false, indicatorReason: '', tag: el ? el.tagName.toLowerCase() : '',
      id: el ? (el.id || '') : '', sig: el ? NS._sig(el) : '' };
    if (!el || !contained) { NS._modalVisits.push(-1); return out; }
    let a = el.getAttribute('data-kbgate-m');
    if (a === null || a === '') { a = String(NS._modalNext++); el.setAttribute('data-kbgate-m', a); }
    const idx = parseInt(a, 10); out.idx = idx;
    if (NS._modalVisits.length > 0 && idx === NS._modalVisits[0]) out.cycled = true;
    NS._modalVisits.push(idx);
    const focused = NS._snap(el);
    const blurred = NS._modalItems[idx] ? NS._modalItems[idx].blurred : null;
    const reason = NS._hasIndicator(focused, blurred);
    out.hasIndicator = reason !== ''; out.indicatorReason = reason;
    const r = el.getBoundingClientRect(); const cs = getComputedStyle(el);
    out.visible = cs.display !== 'none' && !HIDDEN_VIS[cs.visibility] && r.width > 0 && r.height > 0 &&
      r.bottom > 0 && r.right > 0 && r.top < (window.innerHeight || document.documentElement.clientHeight) &&
      r.left < (window.innerWidth || document.documentElement.clientWidth);
    return out;
  };
  return { ok: true };
})();`

type kbgateTabbable struct {
	I       int         `json:"i"`
	Tag     string      `json:"tag"`
	ID      string      `json:"id"`
	Cls     string      `json:"cls"`
	Sel     string      `json:"sel"`
	Blurred kbgateStyle `json:"blurred"`
}

type kbgateStyle struct {
	OutlineStyle    string  `json:"outlineStyle"`
	OutlineWidth    float64 `json:"outlineWidth"`
	BoxShadow       string  `json:"boxShadow"`
	BorderTopColor  string  `json:"borderTopColor"`
	BorderTopWidth  float64 `json:"borderTopWidth"`
	BackgroundColor string  `json:"backgroundColor"`
	TextDecoration  string  `json:"textDecoration"`
}

type kbgateEnum struct {
	Count int              `json:"count"`
	Items []kbgateTabbable `json:"items"`
}

type kbgateFocus struct {
	Idx             int    `json:"idx"`
	Cycled          bool   `json:"cycled"`
	Visible         bool   `json:"visible"`
	InViewport      bool   `json:"inViewport"`
	W               int    `json:"w"`
	H               int    `json:"h"`
	HasIndicator    bool   `json:"hasIndicator"`
	IndicatorReason string `json:"indicatorReason"`
	NewEl           bool   `json:"newEl"`
	Tag             string `json:"tag"`
	ID              string `json:"id"`
	Cls             string `json:"cls"`
	Sig             string `json:"sig"`
}

type kbgateReport struct {
	Page          string
	Count         int
	Presses       int
	Cycled        bool
	Visits        []kbgateFocus
	FailVisible   []string
	FailIndicator []string
	Missing       []string
	Trap          bool
	Dynamic       []string
}

func kbgateWalk(t *testing.T, ctx context.Context, maxPresses int) (kbgateEnum, []kbgateFocus) {
	t.Helper()
	var enum kbgateEnum
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(kbgateSetupJS, nil),
		chromedp.Evaluate(`window.__kbgate.enumerate()`, &enum),
	); err != nil {
		t.Fatalf("kbgate enumerate: %v", err)
	}
	var visits []kbgateFocus
	for i := 0; i < maxPresses; i++ {
		if err := chromedp.Run(ctx, chromedp.KeyEvent(kb.Tab)); err != nil {
			t.Fatalf("tab press %d: %v", i, err)
		}
		var f kbgateFocus
		if err := chromedp.Run(ctx,
			chromedp.Sleep(25*time.Millisecond),
			chromedp.Evaluate(`window.__kbgate.recordFocus()`, &f),
		); err != nil {
			t.Fatalf("record focus %d: %v", i, err)
		}
		visits = append(visits, f)
		if f.Cycled {
			break
		}
	}
	return enum, visits
}

func kbgateAnalyze(page string, enum kbgateEnum, visits []kbgateFocus) kbgateReport {
	r := kbgateReport{Page: page, Count: enum.Count, Presses: len(visits)}
	visited := make(map[int]bool)
	seenFail := make(map[int]bool)
	for _, f := range visits {
		if f.Idx >= 0 {
			visited[f.Idx] = true
		}
		if f.Cycled {
			r.Cycled = true
		}
		if f.NewEl && f.Idx >= enum.Count {
			r.Dynamic = append(r.Dynamic, f.Sig)
		}
		if f.Idx >= 0 && !seenFail[f.Idx] {
			seenFail[f.Idx] = true
			label := kbgateLabel(enum, f)
			if !f.Visible {
				r.FailVisible = append(r.FailVisible, label+
					" (w="+strconv.Itoa(f.W)+",h="+strconv.Itoa(f.H)+",inVP="+strconv.FormatBool(f.InViewport)+")")
			}
			if !f.HasIndicator && !f.NewEl {
				r.FailIndicator = append(r.FailIndicator, label+" — no visible focus indicator")
			}
		}
	}
	for i := 0; i < enum.Count; i++ {
		if !visited[i] {
			r.Missing = append(r.Missing, kbgateLabelIdx(enum, i))
		}
	}
	r.Trap = !r.Cycled && len(r.Missing) > 0
	return r
}

func kbgateLabel(enum kbgateEnum, f kbgateFocus) string {
	if f.Idx >= 0 && f.Idx < len(enum.Items) {
		return "#" + strconv.Itoa(f.Idx) + " " + enum.Items[f.Idx].Sel
	}
	return "#" + strconv.Itoa(f.Idx) + " " + f.Sig
}

func kbgateLabelIdx(enum kbgateEnum, i int) string {
	if i >= 0 && i < len(enum.Items) {
		return "#" + strconv.Itoa(i) + " " + enum.Items[i].Sel
	}
	return "#" + strconv.Itoa(i)
}

func reportGate(t *testing.T, r kbgateReport) {
	t.Helper()
	t.Logf("[%s] tabbables=%d presses=%d cycled=%v dynamic=%d",
		r.Page, r.Count, r.Presses, r.Cycled, len(r.Dynamic))
	for _, d := range r.Dynamic {
		t.Logf("[%s] dynamic focusable (not in initial tab order): %s", r.Page, d)
	}
	failed := false
	if r.Count == 0 {
		t.Errorf("[%s] no tabbable elements found — keyboard access is impossible", r.Page)
		return
	}
	if !r.Cycled {
		failed = true
		t.Errorf("[%s] TERMINATION: focus did not cycle back to start within %d presses", r.Page, r.Presses)
	}
	if r.Trap {
		failed = true
		t.Errorf("[%s] FOCUS TRAP: %d tabbable element(s) never reached: %s",
			r.Page, len(r.Missing), strings.Join(r.Missing, ", "))
	}
	for _, m := range r.Missing {
		if r.Cycled {
			failed = true
			t.Errorf("[%s] COMPLETENESS: reachable-in-DOM but never focused via Tab: %s", r.Page, m)
		}
	}
	for _, v := range r.FailVisible {
		failed = true
		t.Errorf("[%s] VISIBILITY: focused element not visible: %s", r.Page, v)
	}
	for _, ind := range r.FailIndicator {
		failed = true
		t.Errorf("[%s] FOCUS-INDICATOR: %s", r.Page, ind)
	}
	if !failed {
		t.Logf("[%s] PASS — all keyboard gates green", r.Page)
	}
}

// ─── Page gates ───────────────────────────────────────────────────────

func kbgatePage(t *testing.T, ctx context.Context, base, path string, settle time.Duration) {
	t.Helper()
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+path),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.Sleep(settle),
	); err != nil {
		t.Fatalf("navigate %s: %v", path, err)
	}
	enum, visits := kbgateWalk(t, ctx, 250)
	reportGate(t, kbgateAnalyze(path, enum, visits))
}

func TestKeyboardWalkHome(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := e2eBootApp(t)
	ctx := e2eBrowser(t)
	kbgatePage(t, ctx, base, "/", 800*time.Millisecond)
}

func TestKeyboardWalkLogin(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := e2eBootApp(t)
	ctx := e2eBrowser(t)
	kbgatePage(t, ctx, base, "/login", 800*time.Millisecond)
}

func TestKeyboardWalkCustomers(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := e2eBootApp(t)
	ctx := e2eBrowser(t)
	e2eLogin(t, ctx, base)
	kbgatePage(t, ctx, base, "/app/customers", 900*time.Millisecond)
}

// ─── Modal focus trap-then-release ────────────────────────────────────

type modalEnum struct {
	Count int              `json:"count"`
	Items []kbgateTabbable `json:"items"`
	Error string           `json:"error"`
}

type modalFocus struct {
	Contained       bool   `json:"contained"`
	Idx             int    `json:"idx"`
	Cycled          bool   `json:"cycled"`
	Visible         bool   `json:"visible"`
	HasIndicator    bool   `json:"hasIndicator"`
	IndicatorReason string `json:"indicatorReason"`
	Tag             string `json:"tag"`
	ID              string `json:"id"`
	Sig             string `json:"sig"`
}

// TestModalFocusTrap drives the quick-add modal purely by keyboard: Enter
// opens it, Tab cycles WITHIN it (focus never escapes the widget), Escape
// closes it, and focus returns to the trigger. The modal's own focusables
// also pass the visibility + focus-indicator gates.
func TestModalFocusTrap(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := e2eBootApp(t)
	ctx := e2eBrowser(t)
	e2eLogin(t, ctx, base)

	const triggerSel = `button[data-fui-open="customer-quick-add"]`
	const widgetSel = `[data-fui-widget="customer-quick-add"]`

	// 1. Open the modal with Enter on the trigger (focus positioned, then a
	//    real Enter keypress — Enter on a focused <button> fires click).
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/app/customers"),
		chromedp.WaitVisible(triggerSel, chromedp.ByQuery),
		chromedp.Sleep(800*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('`+triggerSel+`').focus()`, nil),
		chromedp.KeyEvent(kb.Enter),
		chromedp.WaitVisible(`#qa-name`, chromedp.ByQuery),
		chromedp.Sleep(450*time.Millisecond),
	); err != nil {
		t.Fatalf("open modal: %v", err)
	}

	// 2. Enumerate the modal's focusables (blurred snapshot) and Tab-cycle,
	//    asserting focus stays contained every press.
	var menum modalEnum
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(kbgateSetupJS, nil),
		chromedp.Evaluate(`window.__kbgate.enumerateIn('`+widgetSel+`')`, &menum),
	); err != nil {
		t.Fatalf("modal enumerate: %v", err)
	}
	t.Logf("[modal] focusables=%d", menum.Count)
	if menum.Error != "" || menum.Count == 0 {
		t.Fatalf("modal enumerate: error=%q count=%d", menum.Error, menum.Count)
	}

	var escaped []int
	var failVis, failInd []string
	cycled := false
	presses := 0
	for i := 0; i < 40; i++ {
		if err := chromedp.Run(ctx, chromedp.KeyEvent(kb.Tab)); err != nil {
			t.Fatalf("modal tab %d: %v", i, err)
		}
		var mf modalFocus
		if err := chromedp.Run(ctx,
			chromedp.Sleep(25*time.Millisecond),
			chromedp.Evaluate(`window.__kbgate.recordModalFocus()`, &mf),
		); err != nil {
			t.Fatalf("modal record %d: %v", i, err)
		}
		presses++
		if !mf.Contained {
			escaped = append(escaped, i)
		}
		if !mf.Visible && mf.Contained {
			failVis = append(failVis, mf.Sig)
		}
		if !mf.HasIndicator && mf.Contained {
			failInd = append(failInd, mf.Sig)
		}
		if mf.Cycled {
			cycled = true
			break
		}
	}

	// 3. Escape closes the modal and focus returns to the trigger.
	var closed, focusReturned bool
	if err := chromedp.Run(ctx,
		chromedp.KeyEvent(kb.Escape),
		chromedp.Sleep(450*time.Millisecond),
		chromedp.Evaluate(`(() => { const w = document.querySelector('`+widgetSel+`'); return !w || w.hidden || getComputedStyle(w).display === 'none'; })()`, &closed),
		chromedp.Evaluate(`document.activeElement === document.querySelector('`+triggerSel+`')`, &focusReturned),
	); err != nil {
		t.Fatalf("escape: %v", err)
	}

	t.Logf("[modal] presses=%d cycled=%v escaped=%v closed=%v focusReturned=%v",
		presses, cycled, escaped, closed, focusReturned)
	if !cycled {
		t.Errorf("[modal] focus did not cycle within the modal after %d Tab presses (modal focusables=%d) — trap may be wrapping incorrectly or not at all", presses, menum.Count)
	}
	if len(escaped) > 0 {
		t.Errorf("[modal] focus ESCAPED the modal on Tab press(es) %v — focus trap failed (focus must stay within %s while open)", escaped, widgetSel)
	}
	for _, v := range failVis {
		t.Errorf("[modal] VISIBILITY: focused modal element not visible: %s", v)
	}
	for _, ind := range failInd {
		t.Errorf("[modal] FOCUS-INDICATOR: %s — no visible focus indicator", ind)
	}
	if !closed {
		t.Error("[modal] Escape did not close the modal")
	}
	if !focusReturned {
		t.Error("[modal] focus did not return to the trigger after Escape closed the modal")
	}
}
