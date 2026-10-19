package treegate

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/check"
	"github.com/DonaldMurillo/gofastr/core-ui/runtime/minify"
)

// TestRegisteredBehaviorSizeBudgets holds every registered behaviour's
// source to the per-module gzip goal core-ui/runtime's
// TestRuntimeModuleSizeBudgets holds its embedded modules to. Never add
// or raise an override to silence a regression: split or shrink the
// module instead.
//
//gofastr:test-reads importers:github.com/DonaldMurillo/gofastr/core-ui/registry
func TestRegisteredBehaviorSizeBudgets(t *testing.T) {
	// Registered behaviours (docs/spec-behavior-registry.md) are modules
	// from the host down, so the per-module budget holds them too.
	// ModuleNames() cannot: it sees only the registrations linked into
	// THIS test binary, and the packages that register (framework/ui,
	// framework/headless, the examples) live above core-ui/runtime. The
	// budget therefore discovers their sources through the walk every
	// clean-tree gate uses — check.RegisteredBehaviorSources follows the
	// //go:embed beside each RegisterBehavior call in the tree — and
	// holds each FILE to the same goal, minified through the production
	// minifier under the same gate. A registration that escapes the
	// inventory fails rather than skirting the budget: check's own
	// TestRegisteredBehaviorSources_RefusesWhatItCannotRead pins that
	// a source the walk cannot follow is an error (asserted below by
	// failing the test on err), and its
	// TestRegisteredBehaviorSources_FindsTheTreesModules pins the
	// tree's registrations by path, so a new module cannot land without
	// the walk seeing it.
	// A registered behaviour over the goal carries its own row, keyed
	// by its repo path, with the reason it is one unit. Only a module
	// outside core qualifies: it ships in a binary only when its
	// package is imported, and on a page only when asked for.
	behaviorOverrides := map[string]int{
		// core-ui/localdb's on-request API module, 3609 measured (2
		// clearance). 3420 at landing; the second review round added
		// the late-body rejection in tx, closing a connection a failed
		// re-check would leak, broadcasting bulk transactions in
		// slices, and hearing a newer deploy's mixed messages (3577);
		// PR #479's review added coding synchronous request refusals
		// and retrying a failed manifest read. Not in core-ui/runtime and never
		// marker-loaded: only an app that imports core-ui/localdb
		// carries it, and only a page that calls
		// __gofastr.loadModule('localdb') (an app script, or
		// framework/localentity's behaviours through Requires)
		// downloads it. It is one unit because every caller needs all
		// of it on first open: the additive schema reconciler (drift +
		// upgrade + the version race), the transaction wrapper that
		// resolves on COMMIT, the cursor reader, the cross-tab channel
		// with its forged-message guard, and the monotonic UUIDv7
		// minter. Shrunk from 4211 by folding the single-op methods
		// into one loop and dropping the error class.
		"core-ui/localdb/localdb.js": 3611,
	}
	sources, err := check.RegisteredBehaviorSources(repoRoot)
	if err != nil {
		t.Fatalf("registered behaviours: %v", err)
	}
	if len(sources) == 0 {
		t.Fatal("no registered behaviour sources found under the repo root: the walk is broken, not the tree empty")
	}
	for _, f := range sources {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		src := string(raw)
		if minify.Enabled() {
			src = minify.Minify(src)
		}
		budget := ModuleGoalGZ
		rel, _ := filepath.Rel(repoRoot, f)
		if o, ok := behaviorOverrides[filepath.ToSlash(rel)]; ok {
			budget = o
		}
		if got := gzipSize(t, src); got > budget {
			t.Errorf("registered behaviour %s gzip = %d bytes — exceeds %d byte budget (goal %d): split or shrink the module", filepath.Base(f), got, budget, ModuleGoalGZ)
		}
	}
}

// gzipSize is src's size at the compression level browsers receive.
func gzipSize(t *testing.T, s string) int {
	t.Helper()
	var buf bytes.Buffer
	w, err := gzip.NewWriterLevel(&buf, gzip.DefaultCompression)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(s)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Len()
}
