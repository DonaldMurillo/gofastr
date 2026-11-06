package docs

import (
	"strings"
	"testing"
)

func TestOwnerDocsRequireExtractor(t *testing.T) {
	document, err := Get("entity-declarations")
	if err != nil {
		t.Fatal(err)
	}
	for _, stale := range []string{"`OwnerField` is inert", "Without a registered extractor the field is inert"} {
		if strings.Contains(string(document), stale) {
			t.Fatalf("owner docs incorrectly promise fail-open behavior: %s", stale)
		}
	}
	if strings.Count(string(document), "401 until an owner extractor is registered") != 2 {
		t.Fatal("both owner passages must document the fail-closed 401")
	}
}
