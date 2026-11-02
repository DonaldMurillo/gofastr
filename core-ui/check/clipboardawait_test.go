package check

import (
	"path/filepath"
	"testing"
)

// clipboardAwaitFixture: the e2988bd2 pre-fix copy handler (writeText
// with a swallowing .catch, then the Copied label, state and status),
// the fixed copy and selection-copy spellings, and the quiet shapes.
const clipboardAwaitFixture = `(() => {
  document.addEventListener('click', (e) => {
    const wrap = e.target.closest('[data-hui-copy]');
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).catch(() => {});
    }
    wrap.setAttribute('data-hui-copy-state', 'done');
    if (btn && fmts) btn.textContent = fmts;
  });
  function quick(t) {
    if (ok) navigator.clipboard.writeText(t);
    NS.toast({ title: said });
  }
  function voided(t) {
    void navigator.clipboard?.writeText(t);
    btn.textContent = copied;
  }
  document.addEventListener('click', (e) => {
    let write = null;
    try {
      write = navigator.clipboard && navigator.clipboard.writeText ? navigator.clipboard.writeText(text) : null;
    } catch (_) {
      write = null;
    }
    if (!write) { failed(wrap); return; }
    write.then(() => copied(wrap), () => failed(wrap));
  });
  function selection(clip, text) {
    let done;
    if (clip.write) done = clip.write([item]);
    else done = text.then((t) => clip.writeText(t));
    done.then(() => toast({ title: said }), failed);
  }
  async function awaited(t) {
    await navigator.clipboard.writeText(t);
    status.textContent = sentence;
  }
  function chained(t) {
    navigator.clipboard.writeText(t).then(() => { status.textContent = sentence; });
    navigator.clipboard.writeText(t).catch(() => {}).then(() => say('k', 'Copied'));
  }
  function kept(t) { return navigator.clipboard.writeText(t); }
  function silent(t) {
    status.textContent = '';
    navigator.clipboard.writeText(t).catch(() => {});
  }
  function later(t) {
    navigator.clipboard.writeText(t);
  }
  function elsewhere() { status.textContent = sentence; }
  const api = { writeText: (args) => call('clipboard.writeText', args) };
  function pass(t) { report(navigator.clipboard.writeText(t)); status.textContent = sentence; }
  function marked(t) {
    //gofastr:allow(clipboardawait) the dev overlay copies a log line and says nothing that depends on it
    navigator.clipboard.writeText(t);
    btn.setAttribute('aria-pressed', 'true');
  }
})();
`

func TestLintClipboardAwait_FiresOnDroppedWrite(t *testing.T) {
	dir := writeRuntimeFixture(t, "copy.js", clipboardAwaitFixture)
	res, err := LintClipboardAwait(dir)
	if err != nil {
		t.Fatal(err)
	}
	// The pre-fix copy (5), a write behind a bare if (11), a voided
	// optional-chained write (15).
	assertLines(t, res, []int{5, 11, 15})
}

// TestLintClipboardAwait_Repo holds the shipped JavaScript to the rule:
// a copy is announced from the clipboard write's outcome.
//
//gofastr:test-reads core-ui/runtime/
//gofastr:test-reads *.js
func TestLintClipboardAwait_Repo(t *testing.T) {
	if testing.Short() {
		t.Skip("walks the repo")
	}
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Skipf("can't locate repo root: %v", err)
	}
	res, err := LintClipboardAwait(runtimeLintRoots(t, filepath.Join(repoRoot, "core-ui", "runtime"))...)
	if err != nil {
		t.Fatal(err)
	}
	if res.HasErrors() {
		t.Errorf("a copy is announced before the clipboard write settles:\n%s", res.Error())
	}
}
