//gofastr:test-reads core-ui/runtime/frag/ core-ui/runtime/src/

package registry

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Blocks render sorted by id, nil sources render nothing, "</" cannot
// end the script, and every malformed or colliding registration is a
// startup panic.
func TestRegisterDataBlock(t *testing.T) {
	IsolateForTest(t)
	if DataBlocksHTML() != "" {
		t.Fatal("an empty registry rendered a block")
	}
	RegisterDataBlock("gofastr-zeta", func() []byte { return []byte(`{"x":"</script><!--<script>"}`) })
	RegisterDataBlock("gofastr-alpha", func() []byte { return []byte(`{}`) })
	RegisterDataBlock("gofastr-empty", func() []byte { return nil })
	got := DataBlocksHTML()
	want := `<script type="application/json" id="gofastr-alpha">{}</script>` +
		`<script type="application/json" id="gofastr-zeta">{"x":"\u003c/script>\u003c!--\u003cscript>"}</script>`
	if got != want {
		t.Fatalf("DataBlocksHTML =\n%s\nwant\n%s", got, want)
	}

	mustPanic(t, "must match", func() { RegisterDataBlock("localdb", func() []byte { return nil }) })
	mustPanic(t, "must match", func() { RegisterDataBlock(`gofastr-x"><script>`, func() []byte { return nil }) })
	mustPanic(t, "non-nil", func() { RegisterDataBlock("gofastr-nil", nil) })
	mustPanic(t, "registered twice", func() { RegisterDataBlock("gofastr-alpha", func() []byte { return nil }) })
	for _, id := range []string{"gofastr-signals", "gofastr-routes", "gofastr-embed-root"} {
		mustPanic(t, "emits itself", func() { RegisterDataBlock(id, func() []byte { return nil }) })
	}
	if strings.Contains(DataBlocksHTML(), "gofastr-nil") {
		t.Fatal("a refused registration landed")
	}
}

// Every element id the runtime looks up is one the host emits, so an
// app block under it would shadow the real one (#gofastr-locales once
// went unreserved).
func TestRuntimeIDsReserved(t *testing.T) {
	read := regexp.MustCompile(`(?:_readInlineJSON|getElementById)\(['"](gofastr-[a-z0-9-]+)`)
	var ids int
	for _, dir := range []string{"../runtime/frag", "../runtime/src"} {
		files, err := filepath.Glob(filepath.Join(dir, "*.js"))
		if err != nil || len(files) == 0 {
			t.Fatalf("glob %s: %v (%d files)", dir, err, len(files))
		}
		for _, f := range files {
			src, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			for _, m := range read.FindAllStringSubmatch(string(src), -1) {
				ids++
				if !reservedDataBlockIDs[m[1]] {
					t.Errorf("%s reads #%s, which RegisterDataBlock does not reserve", filepath.Base(f), m[1])
				}
			}
		}
	}
	if ids == 0 {
		t.Fatal("found no runtime element-id reads: the pattern is stale")
	}
}
