package chromepdf

import (
	"os"
	"strings"
	"testing"
)

func TestAllowedHostsRespectsCSPDocs(t *testing.T) {
	source, err := os.ReadFile("chromepdf.go")
	if err != nil {
		t.Fatal(err)
	}
	_, comment, _ := strings.Cut(string(source), "// AllowedHosts ")
	comment, _, _ = strings.Cut(comment, "AllowedHosts []string")
	for _, want := range []string{"does not widen", "images", "fonts", "BaseURL"} {
		if !strings.Contains(comment, want) {
			t.Errorf("AllowedHosts godoc lacks %q: %s", want, comment)
		}
	}
}
