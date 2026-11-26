package mediatype

import (
	"fmt"
	"strings"
	"testing"
)

func TestBrowserChecksAreKeyedByMarker(t *testing.T) {
	const otherMarker = "/* gofastr:other-response-check */"
	const otherCheck = "const isOther = () => true;\n"
	checks := parseBrowserChecks(browserCheckSource + otherMarker + "\n" + otherCheck)
	if got := checks[otherMarker]; got != otherCheck {
		t.Errorf("third check = %q, want %q", got, otherCheck)
	}
	if got := checks[htmlResponseCheckMarker]; got != htmlResponseCheck+"\n" {
		t.Errorf("HTML check changed after appending a third check: %q", got)
	}
	const jsonCheck = "const isJSON = r => /application\\/json/i.test(r.headers.get('content-type'));\n"
	if got := WithJSONResponseCheck(jsonResponseCheckMarker); got != jsonCheck {
		t.Errorf("JSON splice = %q, want %q", got, jsonCheck)
	}
}

func TestWithJSONResponseCheckPanicsWithoutExactlyOneMarker(t *testing.T) {
	testResponseCheckMarkerCount(t, jsonResponseCheckMarker, WithJSONResponseCheck)
}

func TestWithHTMLResponseCheckPanicsWithoutExactlyOneMarker(t *testing.T) {
	testResponseCheckMarkerCount(t, htmlResponseCheckMarker, WithHTMLResponseCheck)
}

func testResponseCheckMarkerCount(t *testing.T, marker string, inject func(string) string) {
	t.Helper()
	for _, tc := range []struct {
		name, script string
		count        int
	}{
		{"missing", "(() => {})();", 0},
		{"duplicate", marker + "\n" + marker, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				got := recover()
				if got == nil {
					t.Fatal("response check injection did not panic")
				}
				if want := fmt.Sprintf("found %d", tc.count); !strings.HasSuffix(fmt.Sprint(got), want) {
					t.Errorf("panic = %q, want suffix %q", got, want)
				}
			}()
			inject(tc.script)
		})
	}
}
