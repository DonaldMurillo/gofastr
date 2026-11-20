package gtk

import (
	"strings"
	"testing"
)

func TestMissingLibraryNamesThePackages(t *testing.T) {
	saved, savedErr := libraries, loadErr
	t.Cleanup(func() { libraries, loadErr = saved, savedErr })
	libraries = []*library{{soname: "libgofastr-absent-optional.so.0", optional: true}}
	loadErr = nil
	load()
	if loadErr != nil {
		t.Fatalf("a missing optional library failed Load: %v", loadErr)
	}
	libraries = []*library{{soname: "libgofastr-absent.so.0", packages: "gofastr-absent"}}
	load()
	if !Missing(loadErr) || !strings.Contains(loadErr.Error(), installLine) {
		t.Fatalf("a missing required library = %v, want ErrUnavailable naming %q", loadErr, installLine)
	}
}
