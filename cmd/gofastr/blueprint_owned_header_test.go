package main

import (
	"maps"
	"slices"
	"strings"
	"testing"
)

// A generated app's files are the app's own code, so they carry no
// "Code generated" header: agent tooling reads that line as
// do-not-edit and refused to touch main.go in the third layout eval.
// Only the files gofastr regenerates (the _style/_tokens .gen.go
// files) say "Code generated", and they say DO NOT EDIT with it.
// Meridian's blueprint covers entities, auth and marketing, so every
// owned-file emitter runs.
func TestBlueprintOwnedFilesHaveNoGeneratedHeader(t *testing.T) {
	bp, err := loadBlueprint("../../examples/meridian/gofastr.yml")
	if err != nil {
		t.Fatalf("loadBlueprint: %v", err)
	}
	files := filesByName(mustRenderBlueprintFiles(t, bp))
	for _, want := range []string{"main.go", "resource.go", "e2e_test.go"} {
		if files[want] == "" {
			t.Fatalf("fixture no longer emits %s; the check would not cover it", want)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(files)) {
		src := files[name]
		if strings.Contains(src, "Code generated") && !strings.Contains(src, "DO NOT EDIT") {
			t.Errorf("%s is the app's own file but carries a generated-code header", name)
		}
	}
}
