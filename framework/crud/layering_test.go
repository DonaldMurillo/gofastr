package crud_test

import (
	"os/exec"
	"strings"
	"testing"
)

// The image pipeline reaches the upload path through the file.ImageDeriver
// interface, implemented in framework/imagefield. The indirection exists for
// exactly one reason: framework/image carries every image decoder plus the
// WebP encoder, and crud is in the dependency graph of essentially every
// GoFastr application. A direct edge would put all of those codecs in every
// binary, whether or not the app touches images.
//
// This is the enforceable form of that rule. A future "just import it here,
// it's simpler" refactor fails here instead of quietly growing every
// downstream binary.
func TestUploadPathDoesNotLinkImageCodecs(t *testing.T) {
	const banned = "github.com/DonaldMurillo/gofastr/framework/image"
	for _, pkg := range []string{
		"github.com/DonaldMurillo/gofastr/framework/crud",
		"github.com/DonaldMurillo/gofastr/framework/file",
	} {
		out, err := exec.Command("go", "list", "-deps", pkg).CombinedOutput()
		if err != nil {
			t.Fatalf("go list -deps %s: %v\n%s", pkg, err, out)
		}
		for line := range strings.SplitSeq(string(out), "\n") {
			if strings.TrimSpace(line) == banned {
				t.Errorf("%s depends on %s — implement file.ImageDeriver in "+
					"framework/imagefield instead of importing the pipeline here", pkg, banned)
			}
		}
	}
}
