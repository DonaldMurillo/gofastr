// Package nojstest checks that a GoFastr page still works with
// JavaScript turned off: the server's HTML alone carries the content,
// the navigation and the forms, and the runtime only enhances them.
//
// [Check] loads a page in a tab whose script execution is disabled
// through the DevTools protocol before the first byte arrives, so
// runtime.js, the app's own scripts and inline scripts never run, then
// reports what a visitor without script would hit:
//
//   - "empty-main": <main> (or <body> without one) shows no text;
//   - "dead-link": a visible link that needs script to go anywhere
//     (href="#", a javascript: URL, or an <a> with no href acting as a
//     button);
//   - "form-no-submit": a visible form with no native way to submit
//     (no enabled submit button, and not a single-field form Enter
//     submits), or whose action is "#" or a javascript: URL;
//   - "http-status": the page answered 4xx or 5xx, so whatever it shows
//     is an error page, not the page;
//   - "script-ran": the GoFastr runtime executed, so the check itself
//     did not hold and every other result is meaningless.
//
// Each app keeps its own page list and gate Test function, the way
// axetest is used: derive the list from the same source the screens
// register from so a new screen is checked without a test edit.
package nojstest

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
)

// Problem is one way a page fails without script.
type Problem struct {
	// Kind is "empty-main", "dead-link", "form-no-submit",
	// "http-status" or "script-ran".
	Kind string `json:"kind"`
	// Detail names the element (an opening-tag snippet) or the fact.
	Detail string `json:"detail"`
}

func (p Problem) String() string { return p.Kind + ": " + p.Detail }

// checkTimeout bounds one Check or Inspect, not the tab: a gate runs
// hundreds of pages through one tab.
const checkTimeout = 30 * time.Second

// NewTab opens a tab on browser with script execution disabled. Every
// navigation in it, including a native form submission or a followed
// link, loads with script off. Cancel it when done.
func NewTab(t *testing.T, browser context.Context) (context.Context, context.CancelFunc) {
	t.Helper()
	tab, cancel := chromedp.NewContext(browser)
	// The first Run opens the target and ties its life to the context it
	// is given, so it runs on tab itself, never a timeout child.
	if err := chromedp.Run(tab, emulation.SetScriptExecutionDisabled(true)); err != nil {
		cancel()
		t.Fatalf("nojstest: disable script execution: %v", err)
	}
	return tab, cancel
}

// Check navigates tab (from [NewTab]) to url and returns what fails
// there without script, including an error status on the response. An
// error means the page could not be loaded or read, not that it has
// problems.
func Check(tab context.Context, url string) ([]Problem, error) {
	ctx, done := context.WithTimeout(tab, checkTimeout)
	defer done()
	resp, err := chromedp.RunResponse(ctx, chromedp.Navigate(url))
	if err == nil {
		err = chromedp.Run(ctx, chromedp.WaitReady("body", chromedp.ByQuery))
	}
	if err != nil {
		return nil, fmt.Errorf("load %s: %w", url, err)
	}
	ps, err := Inspect(tab)
	if resp != nil && resp.Status >= 400 {
		ps = append([]Problem{{Kind: "http-status", Detail: fmt.Sprintf("the page answered %d", resp.Status)}}, ps...)
	}
	return ps, err
}

// Inspect reports the problems on the page tab shows now, without
// navigating: after a native form submission, say, or a followed link.
func Inspect(tab context.Context) ([]Problem, error) {
	ctx, done := context.WithTimeout(tab, checkTimeout)
	defer done()
	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(probeJS, &raw)); err != nil {
		return nil, fmt.Errorf("inspect page: %w", err)
	}
	var out []Problem
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("decode probe result: %w", err)
	}
	return out, nil
}

// Report fails t with one line per problem, labelled with path, and
// reports whether there were any.
func Report(t *testing.T, path string, problems []Problem) bool {
	t.Helper()
	for _, p := range problems {
		t.Errorf("no-JS %s: %s", path, p)
	}
	return len(problems) > 0
}

// probeJS runs through DevTools evaluation, which disabled page script
// does not block. It returns a JSON array of problems.
var probeJS = strings.TrimSpace(`
(() => {
  const out = [];
  const snip = (el) => {
    const s = el.outerHTML.slice(0, el.outerHTML.indexOf('>') + 1);
    return s.length > 160 ? s.slice(0, 160) + '…' : s;
  };
  const shown = (el) => {
    const r = el.getBoundingClientRect();
    if (r.width === 0 && r.height === 0) return false;
    const s = getComputedStyle(el);
    return s.visibility !== 'hidden' && s.display !== 'none';
  };
  if (window.__gofastr) {
    out.push({kind: 'script-ran', detail: 'the GoFastr runtime executed; script execution was not disabled'});
  }
  const main = document.querySelector('main') || document.body;
  // innerText of an element that is not rendered is its raw text, so
  // a hidden <main> needs its own check.
  if (!main || !main.checkVisibility() || !(main.innerText || '').trim()) {
    out.push({kind: 'empty-main', detail: (main ? main.tagName.toLowerCase() : 'body') + ' shows no text'});
  }
  // A control named "elements" shadows form.elements, so read the
  // collection through the prototype. Image buttons are left out of
  // it by the spec and are gathered separately.
  const elementsOf = Object.getOwnPropertyDescriptor(HTMLFormElement.prototype, 'elements').get;
  const controls = (f) => [...elementsOf.call(f),
    ...[...document.querySelectorAll('input[type="image" i]')].filter((el) => el.form === f)];
  // The URL parser drops every tab and newline and trims C0 controls
  // and spaces, so "java\tscript:" is still a javascript: URL.
  const urlOf = (v) => v.replace(/[\t\n\r]/g, '').replace(/^[\x00-\x20]+|[\x00-\x20]+$/g, '');
  for (const a of document.querySelectorAll('a')) {
    if (!shown(a)) continue;
    const href = a.getAttribute('href');
    if (href === null) {
      if (a.getAttribute('role') === 'button' || a.hasAttribute('onclick') ||
          [...a.attributes].some((x) => x.name.startsWith('data-cui-'))) {
        out.push({kind: 'dead-link', detail: snip(a) + ' has no href and acts as a button'});
      }
      continue;
    }
    const h = urlOf(href);
    if (h === '#' || /^javascript:/i.test(h)) {
      out.push({kind: 'dead-link', detail: snip(a) + ' goes nowhere without script'});
    }
  }
  for (const f of document.querySelectorAll('form')) {
    if (!shown(f) || (f.getAttribute('method') || '').toLowerCase() === 'dialog') continue;
    const action = urlOf(f.getAttribute('action') || '');
    // "#" resolves to the current page, which a native submission
    // reaches; only a javascript: action goes nowhere. A submit
    // button's formaction overrides the form's action.
    const dead = (v) => /^javascript:/i.test(v);
    const actionOf = (el) => el.hasAttribute('formaction') ? urlOf(el.getAttribute('formaction')) : action;
    // el.type is the reflected type the browser submits by: an invalid
    // or oddly spelled type attribute on a button still means submit.
    // :disabled also covers a control inside a disabled fieldset.
    const isSubmit = (el) => (el.tagName === 'BUTTON' && el.type === 'submit') ||
      (el.tagName === 'INPUT' && (el.type === 'submit' || el.type === 'image'));
    const submits = controls(f).filter(isSubmit);
    const enabled = submits.filter((el) => !el.matches(':disabled'));
    if (enabled.some((el) => !dead(actionOf(el)))) continue;
    if (enabled.length > 0 || (submits.length === 0 && dead(action))) {
      out.push({kind: 'form-no-submit', detail: snip(f) + ' submits nowhere without script'});
      continue;
    }
    // Enter submits a single-field form only when it has no submit
    // button at all: with one, Enter clicks it, and a disabled one
    // does nothing.
    if (submits.length > 0) {
      out.push({kind: 'form-no-submit', detail: snip(f) + ' has no enabled submit button'});
      continue;
    }
    const blocking = controls(f).filter((el) => el.tagName === 'INPUT' &&
      ['text', 'search', 'email', 'url', 'tel', 'password', 'number', 'date', 'datetime-local', 'month', 'time', 'week'].includes((el.type || 'text').toLowerCase()));
    if (blocking.length === 1) continue; // Enter submits a single-field form
    out.push({kind: 'form-no-submit', detail: snip(f) + ' has no submit button'});
  }
  return JSON.stringify(out);
})()
`)
