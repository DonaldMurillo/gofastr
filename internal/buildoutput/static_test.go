package buildoutput

import (
	"testing"
	"testing/fstest"
)

func TestStaticExportPreservesModuleSource(t *testing.T) {
	for _, module := range []bool{false, true} {
		tree := fstest.MapFS{"preview/__gofastr/runtime.js": &fstest.MapFile{Data: []byte("runtime")}, "preview/__gofastr/app.css": &fstest.MapFile{Data: []byte("css")}}
		if module {
			tree["preview/go.mod"] = &fstest.MapFile{Data: []byte("module app")}
		}
		if got := StaticExport(tree, "preview"); got == module {
			t.Errorf("module=%v: StaticExport=%v", module, got)
		}
	}
}
