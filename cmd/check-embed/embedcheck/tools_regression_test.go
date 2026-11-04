package embedcheck

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

func TestIndirectOptionsAreNotSilentlyClean(t *testing.T) {
	analysistest.Run(t, testdataDir(t), Analyzer, "./src/optionindirection")
}

func TestMemberSpellingsFlagged(t *testing.T) {
	analysistest.Run(t, testdataDir(t), Analyzer, "./src/memberspelling")
}

func TestRenderComputedActionsBlock(t *testing.T) {
	analysistest.Run(t, testdataDir(t), Analyzer, "./src/rendercomputed")
	_, notes := loadAll(t, "rendercomputed")
	if len(notes) != 1 || !notes[0].Blocking {
		t.Fatalf("Render-only computed action not blocking: %+v", notes)
	}
}

func TestRenderComputedActionsNeedBootValueEvidence(t *testing.T) {
	findings, notes := loadAll(t, "rendernilfield")
	if len(findings) != 0 || len(notes) != 6 {
		t.Fatalf("findings=%v notes=%+v; want six computed-action notes", findings, notes)
	}
	for _, note := range notes {
		wantBlocking := note.Surface != "boot-value" && note.Surface != "boot-new"
		if note.Blocking != wantBlocking {
			t.Errorf("surface=%s blocking=%v, want %v: %s", note.Surface, note.Blocking, wantBlocking, note.Reason)
		}
	}
}

func TestEmptyPackageSetIsNotClean(t *testing.T) {
	if _, _, err := checkLoaded(nil); err == nil {
		t.Fatal("empty package set reported clean")
	}
}
