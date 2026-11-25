package mediatype

import "testing"

func TestWithJSONResponseCheckPanicsWithoutExactlyOneMarker(t *testing.T) {
	const marker = "/* gofastr:json-response-check */"
	for _, tc := range []struct {
		name, script string
	}{
		{"missing", "(() => {})();"},
		{"duplicate", marker + "\n" + marker},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("WithJSONResponseCheck did not panic")
				}
			}()
			WithJSONResponseCheck(tc.script)
		})
	}
}
