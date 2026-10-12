package runtime

import (
	"strings"
	"testing"
)

// TestKernelMarkupMatchesGo: the generation the kernel sends in
// X-Gofastr-Markup is the one hosts compare against. A bump on one
// side only would turn every navigation of today's runtime into a
// reload.
func TestKernelMarkupMatchesGo(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	flat := strings.ReplaceAll(js, " ", "")
	if want := "_markup:'" + MarkupVersion + "'"; !strings.Contains(flat, want) {
		t.Fatalf("runtime.js does not declare %s: frag/kernel.js and MarkupVersion disagree", want)
	}
	if want := "'X-Gofastr-Markup':'" + MarkupVersion + "'"; !strings.Contains(flat, want) {
		t.Fatalf("runtime.js does not send %s: frag/nav.js and MarkupVersion disagree", want)
	}
}
