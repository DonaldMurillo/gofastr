package check

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// runtimeProseFixture is reduced from the pre-say() tree (2026-10-21):
// nav.js's failed-navigation toast, the kernel's fallback notice labels,
// boot-embed's notices and rpc.js's request toasts, beside the say()
// spellings that replaced them.
const runtimeProseFixture = `(() => {
  'use strict';
  const NS = window.__gofastr;
  function failNav(path, err) {
    _showNavToast(err
      ? 'Could not load ' + path + ' (' + err.message + ')'
      : 'Could not load ' + path + ' — check your connection');
  }
  function notice(c, dismiss, root, kind, code, input) {
    c.setAttribute('aria-label', 'Notifications');
    dismiss.setAttribute('aria-label', 'Dismiss notification');
    root.textContent = kind === 'link'
      ? 'This panel cannot open links.'
      : 'This panel cannot submit this form.';
    input.placeholder = 'Search the docs';
    root.appendChild(document.createTextNode(` + "`${code} panels is the limit`" + `));
    NS._toastOrFallback({ variant: 'error', title: 'Request failed', body: 'Network error' });
    NS.toast({ variant: 'success', title: 'Changes saved' });
    NS._toastOrFallback?.({ variant: 'success', title: kind || 'Done' });
    root.textContent = 'Error: ' + code;
    root.textContent += 'Pick a file';
    root.textContent = input.value.length + ' chars';
    root.textContent = ms < 1000 ? ms + 'ms' : Math.round(ms / 1000) + 's';
  }
  function quiet(c, root, input, r, kind) {
    c.setAttribute('aria-label', say('runtime.notifications', 'Notifications'));
    c.setAttribute('data-cui-state', 'Loading');
    c.setAttribute('aria-label', c.getAttribute('data-label') || '');
    root.textContent = NS.say('runtime.request_failed_status', 'Request failed ({{status}})', { status: r.status });
    root.textContent = kind === 'a' ? 'data-hui-x' : 'runtime.key';
    input.placeholder = '';
    NS.toast({ variant: 'error', title: root.getAttribute(P + 'fail') || '' });
    NS._toastOrFallback({ variant: 'error', title: NS.say('runtime.request_failed', 'Request failed') });
    NS.toast({ variant: 'error', title: NS.say('runtime.request_failed', 'Request failed') });
    // gofastr:allow(runtimeprose) developer diagnostics, never shown to a reader
    root.textContent = 'Plugin host refused the frame';
    if (r.ok) root.title = 'Reload me'; // a comment saying 'Reload me' is not a sink
  }
  function unreasoned(root) {
    // gofastr:allow(runtimeprose)
    root.textContent = 'A bare marker silences nothing';
  }
})();
`

func TestLintRuntimeProseFiresOnLiterals(t *testing.T) {
	dir := t.TempDir()
	writeJS(t, dir, "prose.js", runtimeProseFixture)
	res, err := LintRuntimeProse(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"Could not load ", " — check your connection", "Notifications", "Dismiss notification",
		"This panel cannot open links.", "This panel cannot submit this form.", "Search the docs",
		"${code} panels is the limit", "Request failed", "Network error",
		"A bare marker silences nothing", "Reload me", "Changes saved", "Done",
		"Error: ", "Pick a file",
		// A word glued to a value with + is prose too: widgethelpers.js
		// wrote "12 chars" and "340ms" on every locale (2026-10-27 R4). A
		// lone letter ('s') and punctuation (' (') stay quiet.
		" chars", "ms",
	}
	got := map[string]int{}
	for _, v := range res.Violations {
		lit, _, _ := strings.Cut(strings.TrimPrefix(v.Message, `[runtime-prose] "`), `"`)
		if !slices.Contains(want, lit) {
			t.Errorf("fired on a quiet spelling %q: %s", lit, v.Message)
		}
		got[lit]++
	}
	for _, w := range want {
		if got[w] == 0 {
			t.Errorf("no finding for %q (full result:\n%s)", w, res.Error())
		}
	}
	// "Could not load " is written on both arms of the ternary.
	if got["Could not load "] != 2 || len(res.Violations) != len(want)+1 {
		t.Errorf("%d findings, want %d:\n%s", len(res.Violations), len(want)+1, res.Error())
	}
}

// runtimeProseSinksFixture holds the reader-facing sinks the 2026-11-02
// guard bash found the lint blind to: the browser's own dialogs, the
// text-node-creating DOM methods (append('x') is createTextNode('x')
// spelled shorter), the rest of the ARIA text attributes and their
// reflected properties, and nodeValue.
const runtimeProseSinksFixture = `(() => {
  if (!window.confirm('Discard your changes?')) return;
  alert('Upload failed');
  root.append('No results found');
  root.replaceChildren(icon, 'Nothing here yet');
  slider.setAttribute('aria-valuetext', 'Three of five');
  card.setAttribute('aria-roledescription', 'Draggable card');
  slider.ariaValueText = 'Three of five';
  card.ariaRoleDescription = 'Draggable card';
  dlg.ariaDescription = 'Press escape to close';
  node.nodeValue = 'Saved just now';
  NS.confirm(form, sub);
  params.append('_id', id);
  headers.append('Content-Type', 'application/json');
  root.append(make('div', { 'font-weight': '600' }, 'layout layer'));
  card.setAttribute('aria-roledescription', say('k.card', 'Draggable card'));
})();
`

func TestLintRuntimeProseReadsEveryReaderSink(t *testing.T) {
	dir := t.TempDir()
	writeJS(t, dir, "sinks.js", runtimeProseSinksFixture)
	res, err := LintRuntimeProse(dir)
	if err != nil {
		t.Fatal(err)
	}
	assertLines(t, res, []int{2, 3, 4, 5, 6, 7, 8, 9, 10, 11})
}

func TestLintRuntimeProseRepoIsClean(t *testing.T) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Skipf("can't locate repo root: %v", err)
	}
	runtimeDir := filepath.Join(repoRoot, "core-ui", "runtime")
	if _, err := os.Stat(runtimeDir); err != nil {
		t.Skipf("runtime dir not present: %v", err)
	}
	// The example apps are English demos and stay out, as they do for
	// the rawtext analyzer; Meridian, the flagship, is held to it.
	examples := filepath.Join(repoRoot, "examples") + string(filepath.Separator)
	meridian := filepath.Join(examples, "meridian") + string(filepath.Separator)
	roots := slices.DeleteFunc(runtimeLintRoots(t, runtimeDir), func(p string) bool {
		return strings.HasPrefix(p, examples) && !strings.HasPrefix(p, meridian)
	})
	res, err := LintRuntimeProse(roots...)
	if err != nil {
		t.Fatal(err)
	}
	if res.HasErrors() {
		t.Errorf("runtime JS writes reader-facing English outside say():\n%s", res.Error())
	}
}
