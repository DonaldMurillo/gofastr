package mediatype

import (
	"fmt"
	"strings"
	"testing"
)

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
