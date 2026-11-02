package check

import (
	"path/filepath"
	"testing"
)

// keyGuardIMEFixture: the e2988bd2 pre-fix combobox listener (Enter
// behind isComposing alone), the widgethelpers submit-on-Enter shape
// (Enter in a helper, isComposing in the listener), the fixed guard in
// the listener and in a helper, and the shapes that must stay quiet.
const keyGuardIMEFixture = `(() => {
  document.addEventListener('keydown', function (e) {
    const input = e.target.closest('[data-hui-combobox-input]');
    if (!input) return;
    if (e.isComposing) return;
    switch (e.key) {
      case 'ArrowDown': return;
      case 'Enter': {
        pickOption(input);
        return;
      }
    }
  });
  const isEnter = function (e) { return e.key === 'Enter' || e.keyCode === 13; };
  ta.addEventListener('keydown', function (e) {
    if (!isEnter(e) || e.shiftKey || e.isComposing) return;
    form.requestSubmit();
  });
  document.addEventListener('keydown', function (e) {
    if (e.isComposing || e.keyCode === 229) return;
    if (e.key === 'Enter') pick();
  });
  function composing(e) { return e.isComposing || e.keyCode === 229; }
  field.addEventListener('keydown', (e) => {
    if (composing(e)) return;
    if (e.key === 'Enter') commit();
  });
  tree.addEventListener('keydown', function (e) {
    if (e.key === 'Enter') toggle();
  });
  field.addEventListener('keypress', function (e) {
    if (e.key === 'Enter' && !e.isComposing) e.preventDefault();
  });
  field.addEventListener('click', function (e) {
    if (e.isComposing) return;
    if (e.key === 'Enter') commit();
  });
  field.addEventListener('keydown', function (e) {
    if (e.isComposing) return;
    say('k.hint', 'Enter a name');
    if (e.key === 'Escape') clear();
  });
  function onKey(e) {
    if (e.isComposing) return;
    if (e.key === 'Enter') commit();
  }
  box.addEventListener('keydown', onKey);
  //gofastr:allow(keyguard) the field is a numeric code input that never composes
  pin.onkeydown = function (e) { if (e.isComposing) return; if (e.key === 'Enter') go(); };
})();
`

func TestLintKeyGuard_IME(t *testing.T) {
	dir := writeRuntimeFixture(t, "ime.js", keyGuardIMEFixture)
	res, err := LintKeyGuard(dir)
	if err != nil {
		t.Fatal(err)
	}
	// The pre-fix combobox (8), the helper-tested Enter (16), the named
	// listener (45).
	assertLines(t, res, []int{8, 16, 45})
}

// keyGuardRTLFixture: the e2988bd2 pre-fix theme radios (adjacent case
// labels pairing ArrowRight with ArrowDown), the pre-fix sortablelist
// cross-column move (c564224e lost its fix again in a merge), the
// lightbox and tree shapes, the fixes, and the quiet shapes.
const keyGuardRTLFixture = `(() => {
  document.addEventListener('keydown', function (e) {
    switch (e.key) {
      case 'ArrowRight': case 'ArrowDown': j = (i + 1) % n; break;
      case 'ArrowLeft': case 'ArrowUp': j = (i - 1 + n) % n; break;
    }
  });
  document.addEventListener('keydown', (ev) => {
    if (ev.key === 'ArrowLeft' || ev.key === 'ArrowRight') {
      const target = cols[ev.key === 'ArrowLeft' ? idx - 1 : idx + 1];
      target.appendChild(t);
    }
  });
  document.addEventListener('keydown', function (ev) {
    if (ev.key !== 'ArrowLeft' && ev.key !== 'ArrowRight') return;
    step(m, ev.key === 'ArrowLeft' ? -1 : 1);
  });
  document.addEventListener('keydown', function (e) {
    const dir = e.key === 'ArrowRight' ? 1 : -1;
    move(dir);
  });
  document.addEventListener('keydown', function (e) {
    const rtl = getComputedStyle(group).direction === 'rtl';
    switch (e.key) {
      case 'ArrowRight': j = rtl ? prev : next; break;
      case 'ArrowLeft': j = rtl ? next : prev; break;
    }
  });
  document.addEventListener('keydown', (ev) => {
    const target = cols[idx + ((ev.key === 'ArrowRight') === (getComputedStyle(list).direction !== 'rtl') ? 1 : -1)];
  });
  function forward(e, el) { return (e.key === 'ArrowRight') !== (el.closest('[dir]').getAttribute('dir') === 'rtl'); }
  document.addEventListener('keydown', function (e) {
    if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') step(forward(e, track) ? 1 : -1);
  });
  document.addEventListener('keydown', function (e) {
    if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') e.stopPropagation();
    switch (e.key) {
      case 'ArrowLeft': case 'ArrowRight': return;
      case 'ArrowUp': up(); break;
    }
  });
  el.onkeydown = function (e) {
    if (e.key === 'ArrowLeft') prev();
  };
  //gofastr:allow(keyguard) a canvas nudge moves the shape on the screen's axes in every direction
  canvas.addEventListener('keydown', (e) => { if (e.key === 'ArrowLeft') nudge(-1, 0); });
})();
`

func TestLintKeyGuard_RTL(t *testing.T) {
	dir := writeRuntimeFixture(t, "rtl.js", keyGuardRTLFixture)
	res, err := LintKeyGuard(dir)
	if err != nil {
		t.Fatal(err)
	}
	// The theme radios (4), sortablelist (10), lightbox (15), a step
	// named dir (19), the onkeydown handler (44).
	assertLines(t, res, []int{4, 10, 16, 19, 44})
}

// TestLintKeyGuard_Repo holds the shipped JavaScript to both arms.
//
//gofastr:test-reads core-ui/runtime/
//gofastr:test-reads *.js
func TestLintKeyGuard_Repo(t *testing.T) {
	if testing.Short() {
		t.Skip("walks the repo")
	}
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Skipf("can't locate repo root: %v", err)
	}
	res, err := LintKeyGuard(runtimeLintRoots(t, filepath.Join(repoRoot, "core-ui", "runtime"))...)
	if err != nil {
		t.Fatal(err)
	}
	if res.HasErrors() {
		t.Errorf("a keydown listener misses the IME or right-to-left guard:\n%s", res.Error())
	}
}
