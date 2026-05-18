package ui

import (
	"strings"
	"testing"
)

const samplePatch = `--- a/foo.go
+++ b/foo.go
@@ -1,3 +1,4 @@
 package main
-import "fmt"
+import "log"
+import "os"
 func main() {}
`

func TestDiffViewerRequiresPatch(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("DiffViewer without Patch should panic")
		}
	}()
	DiffViewer(DiffViewerConfig{})
}

func TestDiffViewerRejectsUnknownMode(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("DiffViewer with unknown Mode should panic")
		}
	}()
	DiffViewer(DiffViewerConfig{Patch: "x", Mode: DiffMode("rainbow")})
}

func TestDiffViewerUnifiedHasAddAndRemoveLines(t *testing.T) {
	h := string(DiffViewer(DiffViewerConfig{Patch: samplePatch}))
	if !strings.Contains(h, "ui-diff-viewer__line--add") {
		t.Errorf("unified diff should emit add lines:\n%s", h)
	}
	if !strings.Contains(h, "ui-diff-viewer__line--remove") {
		t.Errorf("unified diff should emit remove lines:\n%s", h)
	}
	if !strings.Contains(h, "ui-diff-viewer__line--context") {
		t.Errorf("unified diff should emit context lines:\n%s", h)
	}
}

func TestDiffViewerSplitEmitsHeaderRow(t *testing.T) {
	h := string(DiffViewer(DiffViewerConfig{
		Patch: samplePatch, Mode: DiffSplit,
		LeftLabel: "Before", RightLabel: "After",
	}))
	if !strings.Contains(h, "ui-diff-viewer__header") {
		t.Errorf("split diff should emit header row:\n%s", h)
	}
	if !strings.Contains(h, "Before") || !strings.Contains(h, "After") {
		t.Errorf("split diff should render header labels:\n%s", h)
	}
}

func TestDiffViewerSplitEmitsRowsWithBothCells(t *testing.T) {
	h := string(DiffViewer(DiffViewerConfig{Patch: samplePatch, Mode: DiffSplit}))
	if !strings.Contains(h, "ui-diff-viewer__row") {
		t.Errorf("split diff should emit rows:\n%s", h)
	}
	if !strings.Contains(h, "ui-diff-viewer__cell--add") {
		t.Errorf("split diff should mark add cells:\n%s", h)
	}
	if !strings.Contains(h, "ui-diff-viewer__cell--remove") {
		t.Errorf("split diff should mark remove cells:\n%s", h)
	}
}

func TestDiffViewerHunkHeaderRendersAsHunk(t *testing.T) {
	h := string(DiffViewer(DiffViewerConfig{Patch: samplePatch}))
	if !strings.Contains(h, "ui-diff-viewer__hunk") {
		t.Errorf("@@ hunk header should render as .ui-diff-viewer__hunk:\n%s", h)
	}
}

func TestDiffViewerFileHeaderRenders(t *testing.T) {
	h := string(DiffViewer(DiffViewerConfig{Patch: samplePatch}))
	if !strings.Contains(h, "ui-diff-viewer__file") {
		t.Errorf("--- / +++ headers should render as .ui-diff-viewer__file:\n%s", h)
	}
}
