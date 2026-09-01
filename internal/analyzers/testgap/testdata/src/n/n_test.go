package n

import (
	"strings"
	"testing"
)

func TestAllCovered(t *testing.T) {
	for _, ext := range []string{"woff", "woff2", "ttf"} {
		if !allowFont(ext) {
			t.Fatalf("%s must be allowed", ext)
		}
	}
	if dispatch("stream") != "s" {
		t.Fatal("stream must dispatch")
	}
	if got := strings.Join(blockedTypes, ","); got != "csv,tsv" {
		t.Fatalf("blocked = %s", got)
	}
}
