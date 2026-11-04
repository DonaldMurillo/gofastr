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

func TestEmptyPackageSetIsNotClean(t *testing.T) {
	if _, _, err := checkLoaded(nil); err == nil {
		t.Fatal("empty package set reported clean")
	}
}
