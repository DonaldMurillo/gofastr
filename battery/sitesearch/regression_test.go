package sitesearch

import (
	"bytes"
	"strings"
	"testing"
)

func TestEmptyStaticIndexWritesArrayOfKeys(t *testing.T) {
	files, err := Build(nil).Files()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(files["manifest.json"], []byte(`"keys":[]`)) {
		t.Fatalf("manifest = %s, want iterable empty keys", files["manifest.json"])
	}
}

func TestOneIdentifierContributesEachTermOnce(t *testing.T) {
	scores := map[string]int{}
	addField(scores, "TreeTree", weightBody)
	if scores["tree"] != weightBody {
		t.Fatalf("score for tree = %d, want %d", scores["tree"], weightBody)
	}
}

func TestMixedCapitalizationTitleRetainsCamelPart(t *testing.T) {
	spans, _ := titleSlots("Formfield FormField")
	if _, ok := spans["field"]; !ok {
		t.Fatalf("title slots = %v, want field among the title terms", spans)
	}
}

func TestZeroLimitSearchPageReportsTotal(t *testing.T) {
	idx := Build([]Page{{URL: "/walrus", Title: "Walrus"}})
	page, total := idx.SearchPage("walrus", 0, 0)
	if len(page) != 0 || total != 1 {
		t.Fatalf("SearchPage(walrus, 0, 0) returned %d of %d, want 0 of 1", len(page), total)
	}
}

func TestHeadingLineBreakSeparatesWords(t *testing.T) {
	page, ok, err := Extract("/palette", strings.NewReader("<main><h1>Command<br>Palette</h1></main>"))
	if err != nil || !ok {
		t.Fatalf("Extract: ok = %v, error = %v", ok, err)
	}
	if page.Title != "Command Palette" {
		t.Fatalf("title = %q, want Command Palette", page.Title)
	}
}

func TestExtractTemplateDoesNotSetPageRobotsPolicy(t *testing.T) {
	_, ok, err := Extract("/docs", strings.NewReader(`<template><meta name="robots" content="noindex"></template><main><h1>Docs</h1></main>`))
	if err != nil || !ok {
		t.Fatalf("Extract ok=%t error=%v; inert template metadata must not opt out", ok, err)
	}
}

func TestBuildDeduplicatesImplicitEmptyLead(t *testing.T) {
	index := Build([]Page{
		{URL: "/one", Title: "Title"},
		{URL: "/two", Title: "Title", Sections: []Section{{}}},
	})
	if got := index.Len(); got != 1 {
		t.Fatalf("index has %d copies of the same content, want one", got)
	}
}
