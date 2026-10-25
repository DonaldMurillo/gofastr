package packcmd

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// An edit form is the record page with the unlisted fields omitted, and
// pack reads it back as the edit form with its fields allowlist, so the
// packed spec generates the same app.
func TestEditFormPacksBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gofastr.yml")
	clitest.WriteTestFile(t, path, strings.Replace(clitest.CompositeScreensSpec, "        mode: edit\n", "        mode: edit\n        fields: [grade]\n", 1))
	bp, err := appspec.LoadAppSpec(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	dir := materializeAppSpec(t, bp)
	packed, err := packAppSpec(dir)
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	var edit *appspec.AppSpecBlock
	for _, s := range packed.Screens {
		if s.Name == "enrollment_edit" && len(s.Body) == 1 {
			edit = &s.Body[0]
		}
	}
	if edit == nil || edit.Kind != "entity_form" || edit.Mode != "edit" || !slices.Equal(edit.Fields, []string{"grade"}) || edit.Props != nil {
		t.Fatalf("packed edit form = %+v, want entity_form mode edit fields [grade]", edit)
	}
	if c := packAndCheck(t, dir); len(c.HandWritten) != 0 || len(c.Unrecovered) != 0 {
		t.Fatalf("packed spec does not regenerate the app: %+v", c)
	}
}
