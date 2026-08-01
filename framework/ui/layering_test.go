package ui_test

import (
	"os/exec"
	"strings"
	"testing"
)

// framework/ui renders placeholders that framework/image produces, and the
// temptation is to decode a BlurHash inside the component. It must not:
// framework/image carries every image decoder plus the WebP encoder, so the
// edge would put all of it in the binary of any host that renders any UI at
// all, whether or not it touches images.
//
// The rule was previously only a comment on HeaderInfo in pipeline_image.go.
// A comment does not survive a refactor; this does.
func TestUIDoesNotImportImagePipeline(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", ".").CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps .: %v\n%s", err, out)
	}
	for _, banned := range []string{
		"github.com/DonaldMurillo/gofastr/framework/image",
	} {
		for line := range strings.SplitSeq(string(out), "\n") {
			if strings.TrimSpace(line) == banned {
				t.Errorf("framework/ui depends on %q — decode placeholders in the caller "+
					"and pass framework/ui a finished data URL instead", banned)
			}
		}
	}
}
