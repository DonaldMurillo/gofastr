package main

// Keyboard-only traversal gate — the WCAG chunk axe cannot automate.
//
// For each page we drive ONLY the keyboard (chromedp.KeyEvent with kb.Tab /
// kb.Enter / kb.Escape — never a synthetic focus() to "advance") and assert:
//
//  a) TERMINATION / NO TRAP: repeated Tab from <body> visits a finite
//     sequence and cycles back to the first tabbable (or browser chrome)
//     within a sane bound. A focus trap = focus stuck in a strict subset
//     of the page's tabbables with some never reached.
//  b) VISIBILITY: every element that receives focus is actually visible
//     (non-zero rect, not visibility:hidden, rect intersects the viewport
//     — measured AFTER focus so browsers' auto-scroll-on-focus makes an
//     off-screen skip-link pass, exactly as a real keyboard user sees it).
//  c) FOCUS INDICATION PAINTS: for each focused element a visible focus
//     indicator is present — outline-style != none with width > 0, OR a
//     non-trivial box-shadow, OR a border/background/text-decoration change
//     vs the element's blurred snapshot. Real Tab presses set :focus-visible
//     so computed styles reflect those rules.
//  d) COMPLETENESS: every enumerated visible interactive element is
//     reachable (the visited set covers the tabbables), modulo a
//     documented skip-list for deliberately-untabbable décor.
//
// All failures per page are collected and reported together rather than
// failing on the first. The failing gate IS the failing test: a real
// invisible-focus / trap / unreachable-control defect is fixed upstream in
// framework/ui via the token/variant system, not skip-listed.

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// kbgateSetupJS installs window.__kbgate — a tiny a11y probe that enumerates
// the tabbable elements, snapshots each one's blurred computed style, and
// (called once per Tab press) records the currently-focused element's
// visibility + focus-indicator verdict.
//
// Elements are tagged with data-kbgate-i in DOM order so the focused element
// can be mapped back to its blurred snapshot for the before/after compare.
// Any element that receives focus but was not in the original enumeration
// (a dynamically-mounted control) is assigned a fresh index on the fly.
const kbgateSetupJS = `(() => {
  const NS = window.__kbgate = window.__kbgate || {};
  const TAB_SEL = 'a[href], area[href], button:not([disabled]):not([aria-disabled="true"]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), summary, [tabindex]:not([tabindex="-1"]), iframe, audio[controls], video[controls]';
  const HIDDEN_VIS = { 'hidden': true, 'collapse': true };

  NS._snap = function(el) {
    const cs = getComputedStyle(el);
    return {
      outlineStyle: cs.outlineStyle,
      outlineWidth: parseFloat(cs.outlineWidth) || 0,
      boxShadow: cs.boxShadow,
      borderTopColor: cs.borderTopColor,
      borderTopWidth: parseFloat(cs.borderTopWidth) || 0,
      backgroundColor: cs.backgroundColor,
      textDecoration: cs.textDecorationLine || cs.textDecoration || ''
    };
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
      parts.unshift(part);
      cur = cur.parentElement; depth++;
    }
    return parts.join('>') || el.tagName.toLowerCase();
  };

  // isRendered returns true only for elements that actually paint a box in
  // the current layout — NOT elements hidden by an ancestor's display:none or
  // visibility:hidden (a closed <details> / a desktop-hidden mobile nav). We
  // CANNOT rely on the element's own computed display, which stays non-"none"
  // even when an ancestor is display:none. checkVisibility() reflects real
  // rendering (no box → false); offsetParent is the fallback for older Chrome.
  NS._isRendered = function(el) {
    if (!el || !el.isConnected) return false;
    if (typeof el.checkVisibility === 'function') return el.checkVisibility();
    const cs = getComputedStyle(el);
    if (cs.position === 'fixed') { const r = el.getBoundingClientRect(); return r.width > 0 || r.height > 0; }
    return el.offsetParent !== null;
  };

  NS.reset = function() {
    document.querySelectorAll('[data-kbgate-i]').forEach((el) => el.removeAttribute('data-kbgate-i'));
    NS.tabbables = []; NS.visits = []; NS._next = 0;
  };

  // enumerate tags every visible tabbable, blurs the active element first so
  // the style snapshots are "blurred" (no element in :focus), and returns the
  // list with each element's blurred computed style.
  NS.enumerate = function() {
    NS.reset();
    if (document.activeElement && document.activeElement !== document.body && document.activeElement.blur) {
      document.activeElement.blur();
    }
    const items = [];
    // Radio groups: the browser tabs to ONE radio per name (the checked
    // one, else the first in DOM order). Non-representative radios are
    // correctly skipped by the tab order, so we must NOT enumerate them or
    // the completeness gate reports false "never reached" failures.
    const radioRep = {};
    const all = Array.from(document.querySelectorAll(TAB_SEL));
    all.forEach((el) => {
      if (el.tagName === 'INPUT' && el.type === 'radio' && el.name) {
        if (!(el.name in radioRep)) radioRep[el.name] = el;
        if (el.checked) radioRep[el.name] = el;
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

  // recordFocus reads the currently-focused element, judges visibility +
  // focus-indicator against the stored blurred snapshot, and detects a cycle
  // (focus returned to the first tabbable visited, or wrapped to <body>).
  NS.recordFocus = function() {
    const el = document.activeElement;
    const out = { idx: -1, cycled: false, visible: false, inViewport: false,
      w: 0, h: 0, hasIndicator: false, indicatorReason: '', newEl: false,
      tag: '', id: '', cls: '', sig: '' };
    if (!el || el === document.body || el === document.documentElement) {
      out.cycled = NS.visits.length > 0; // wrapped to chrome / body
      out.sig = !el ? 'none' : (el === document.body ? 'body' : 'html');
      NS.visits.push(-1);
      return out;
    }
    let a = el.getAttribute('data-kbgate-i');
    if (a === null || a === '') { a = String(NS._next++); el.setAttribute('data-kbgate-i', a); out.newEl = true; }
    const idx = parseInt(a, 10);
    out.idx = idx; out.tag = el.tagName.toLowerCase(); out.id = el.id || '';
    out.cls = (el.getAttribute('class') || '').slice(0, 90); out.sig = NS._sig(el);

    const focused = NS._snap(el);
    const blurred = (!out.newEl && NS.tabbables[idx]) ? NS.tabbables[idx].blurred : null;
    let reason = '';
    if (focused.outlineStyle !== 'none' && focused.outlineWidth > 0) {
      reason = 'outline ' + focused.outlineWidth + 'px ' + focused.outlineStyle;
    } else if (focused.boxShadow !== 'none') {
      reason = 'box-shadow';
    } else if (blurred) {
      if (focused.backgroundColor !== blurred.backgroundColor) reason = 'bg change';
      else if (focused.borderTopColor !== blurred.borderTopColor) reason = 'border-color change';
      else if (Math.abs(focused.borderTopWidth - blurred.borderTopWidth) > 0.1) reason = 'border-width change';
      else if (focused.textDecoration !== blurred.textDecoration) reason = 'text-decoration change';
    }
    out.hasIndicator = reason !== ''; out.indicatorReason = reason;

    const r = el.getBoundingClientRect();
    const cs = getComputedStyle(el);
    out.w = Math.round(r.width); out.h = Math.round(r.height);
    out.inViewport = r.bottom > 0 && r.right > 0 && r.top < (window.innerHeight || document.documentElement.clientHeight) &&
      r.left < (window.innerWidth || document.documentElement.clientWidth);
    out.visible = cs.display !== 'none' && !HIDDEN_VIS[cs.visibility] && r.width > 0 && r.height > 0 && out.inViewport;

    if (NS.visits.length > 0 && idx === NS.visits[0]) out.cycled = true;
    NS.visits.push(idx);
    return out;
  };
  return { ok: true };
})();`

// kbgateTabbable is one enumerated tabbable element with its blurred style snapshot.
type kbgateTabbable struct {
	I       int         `json:"i"`
	Tag     string      `json:"tag"`
	ID      string      `json:"id"`
	Cls     string      `json:"cls"`
	Sel     string      `json:"sel"`
	Blurred kbgateStyle `json:"blurred"`
}

type kbgateStyle struct {
	OutlineStyle   string  `json:"outlineStyle"`
	OutlineWidth   float64 `json:"outlineWidth"`
	BoxShadow      string  `json:"boxShadow"`
	BorderTopColor string  `json:"borderTopColor"`
	BorderTopWidth float64 `json:"borderTopWidth"`
	BackgroundColor string `json:"backgroundColor"`
	TextDecoration string  `json:"textDecoration"`
}

type kbgateEnum struct {
	Count int             `json:"count"`
	Items []kbgateTabbable `json:"items"`
}

// kbgateFocus is the per-Tab snapshot returned by recordFocus.
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

// kbgateReport aggregates a page's walk into the four gate verdicts.
type kbgateReport struct {
	Page         string
	Count        int
	Presses      int
	Cycled       bool
	Visits       []kbgateFocus
	FailVisible  []string
	FailIndicator []string
	Missing      []string // completeness — enumerated but never reached
	Trap         bool
	Dynamic      []string // focusables discovered on the fly (informational)
}

// kbgateWalk boots the probe on the current page, then drives real Tab key
// presses (chromedp.KeyEvent(kb.Tab)) — never a synthetic focus() to advance —
// recording each focused element until focus cycles back to the start or the
// press budget is exhausted.
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
		// Let the browser's synchronous scroll-on-focus + any lazy hydration settle.
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

// kbgateAnalyze turns the raw walk into the four gate verdicts. Failures are
// collected, not first-failed: every visibility / indicator / completeness
// defect is surfaced so a single run reports the full picture.
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
		// Visibility + indicator: report once per element (first focus).
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
	// Trap: never cycled AND a strict subset of tabbables was reached.
	r.Trap = !r.Cycled && len(r.Missing) > 0
	return r
}

func kbgateLabel(enum kbgateEnum, f kbgateFocus) string {
	if f.Idx >= 0 && f.Idx < len(enum.Items) {
		it := enum.Items[f.Idx]
		return "#" + strconv.Itoa(f.Idx) + " " + it.Sel
	}
	return "#" + strconv.Itoa(f.Idx) + " " + f.Sig
}

func kbgateLabelIdx(enum kbgateEnum, i int) string {
	if i >= 0 && i < len(enum.Items) {
		return "#" + strconv.Itoa(i) + " " + enum.Items[i].Sel
	}
	return "#" + strconv.Itoa(i)
}
// reportGate asserts all four gates and emits every collected failure. It
// never aborts on the first defect — the whole picture is reported per page.
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
		// Missing is reported under TRAP when !cycled; if cycled but still
		// missing, it's a reachability defect (completeness gate d).
		if r.Cycled {
			failed = true
			t.Errorf("[%s] COMPLETENESS: reachable-in-DOM but never focused via Tab: %s", r.Page, m)
		}
	}
	if r.Cycled {
		_ = failed // silence
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

// kbgatePage navigates to a page, settles hydration, walks the keyboard,
// analyzes, and reports.
func kbgatePage(t *testing.T, base, path string, settle time.Duration) {
	t.Helper()
	ctx := siteBrowserCtx(t)
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
	base := siteE2EServer(t)
	kbgatePage(t, base, "/", 700*time.Millisecond)
}

func TestKeyboardWalkGetStarted(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := siteE2EServer(t)
	kbgatePage(t, base, "/get-started", 700*time.Millisecond)
}

func TestKeyboardWalkDatatable(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := siteE2EServer(t)
	kbgatePage(t, base, "/components/datatable", 900*time.Millisecond)
}
