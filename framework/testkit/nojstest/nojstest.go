// Package nojstest checks that a page works with JavaScript off: [NewTab]
// opens a tab with script disabled and [Check] reports each [Problem].
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
	// Kind is empty-main, dead-link, form-no-submit, http-status or script-ran.
	Kind string `json:"kind"`
	// Detail names the element (an opening-tag snippet) or the fact.
	Detail string `json:"detail"`
}

func (p Problem) String() string { return p.Kind + ": " + p.Detail }

const checkTimeout = 30 * time.Second

// NewTab opens a tab whose every navigation runs with script disabled.
func NewTab(t *testing.T, browser context.Context) (context.Context, context.CancelFunc) {
	t.Helper()
	tab, cancel := chromedp.NewContext(browser)
	if err := chromedp.Run(tab, emulation.SetScriptExecutionDisabled(true)); err != nil {
		cancel()
		t.Fatalf("nojstest: disable script execution: %v", err)
	}
	return tab, cancel
}

// Check loads url in tab and returns what fails there without script; an
// error means the page could not be loaded or read.
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

// Inspect reports the problems on the page tab shows now, without navigating.
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

// Report fails t once per problem, labelled with path, and reports any.
func Report(t *testing.T, path string, problems []Problem) bool {
	t.Helper()
	for _, p := range problems {
		t.Errorf("no-JS %s: %s", path, p)
	}
	return len(problems) > 0
}

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
  if (!main || !main.checkVisibility() || !(main.innerText || '').trim()) {
    out.push({kind: 'empty-main', detail: (main ? main.tagName.toLowerCase() : 'body') + ' shows no text'});
  }
  const elementsOf = Object.getOwnPropertyDescriptor(HTMLFormElement.prototype, 'elements').get;
  const controls = (f) => [...elementsOf.call(f),
    ...[...document.querySelectorAll('input[type="image" i]')].filter((el) => el.form === f)];
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
    const dead = (v) => /^javascript:/i.test(v);
    const actionOf = (el) => el.hasAttribute('formaction') ? urlOf(el.getAttribute('formaction')) : action;
    const isSubmit = (el) => (el.tagName === 'BUTTON' && el.type === 'submit') ||
      (el.tagName === 'INPUT' && (el.type === 'submit' || el.type === 'image'));
    const submits = controls(f).filter(isSubmit);
    const enabled = submits.filter((el) => !el.matches(':disabled'));
    if (enabled.some((el) => !dead(actionOf(el)))) continue;
    if (enabled.length > 0 || (submits.length === 0 && dead(action))) {
      out.push({kind: 'form-no-submit', detail: snip(f) + ' submits nowhere without script'});
      continue;
    }
    if (submits.length > 0) {
      out.push({kind: 'form-no-submit', detail: snip(f) + ' has no enabled submit button'});
      continue;
    }
    const blocking = controls(f).filter((el) => el.tagName === 'INPUT' &&
      ['text', 'search', 'email', 'url', 'tel', 'password', 'number', 'date', 'datetime-local', 'month', 'time', 'week'].includes((el.type || 'text').toLowerCase()));
    if (blocking.length === 1) continue;
    out.push({kind: 'form-no-submit', detail: snip(f) + ' has no submit button'});
  }
  return JSON.stringify(out);
})()
`)
