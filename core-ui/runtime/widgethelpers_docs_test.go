package runtime

import (
	"os"
	"strings"
	"testing"
)

//gofastr:test-reads /core-ui/ARCHITECTURE.md /framework/docs/content/runtime-contract.md
func TestHelperDocsMatchRuntime(t *testing.T) {
	for _, path := range []string{"../ARCHITECTURE.md", "../../framework/docs/content/runtime-contract.md"} {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, check := range []struct {
			attribute string
			want      []string
		}{
			{"data-cui-charcount-source", []string{`data-cui-charcount-source="<selector>"`, "CSS selector"}},
			{"data-cui-fill-input", []string{`data-cui-fill-text="<text>"`, "literal", "text content"}},
			{"data-cui-disable-when-invalid", []string{"On a submit button or image input", "On a form"}},
			{"data-cui-tick-elapsed", []string{"200 ms", "tenths", "milliseconds"}},
		} {
			var row string
			for _, line := range strings.Split(string(body), "\n") {
				if strings.HasPrefix(line, "| `"+check.attribute) {
					row = line
					break
				}
			}
			for _, want := range check.want {
				if !strings.Contains(row, want) {
					t.Errorf("%s: %s row must include %q", path, check.attribute, want)
				}
			}
		}
	}
}
