package appspec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A fragment (no app: section, as generate app --add reads) does not say
// the project's api_prefix, so a screen at /posts beside a posts entity is
// not judged against it; a full spec that sets api_prefix: "" is.
func TestScreenRouteEntityAPICheckNeedsTheAppSection(t *testing.T) {
	const body = "entities:\n  - name: posts\n    crud: true\n    fields:\n      - name: title\n        type: string\nscreens:\n  - name: page\n    route: /posts\n    body:\n      - type: heading\n        text: Page\n"
	dir := t.TempDir()
	fragment, full := filepath.Join(dir, "fragment.yml"), filepath.Join(dir, "full.yml")
	if err := os.WriteFile(fragment, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("app:\n  name: Full\n  api_prefix: \"\"\n"+body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAppSpec(fragment); err != nil {
		t.Errorf("fragment refused: %v", err)
	}
	if _, err := LoadAppSpec(full); err == nil || !strings.Contains(err.Error(), "CRUD API mounts") {
		t.Errorf("full spec with api_prefix \"\" and a screen at /posts: %v", err)
	}
}
