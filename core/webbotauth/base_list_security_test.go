package webbotauth

import (
	"net/http"
	"testing"
)

// A Structured Fields List is comma-separated; an Inner List is
// parenthesised and space-separated. Both ";bs" and ";sf" produce Lists,
// and both reached for the Inner List serializer, so the signature base
// carried "(a b)" where every conformant signer wrote "a, b" — a valid
// signature failing verification, with no test noticing because nothing
// exercised a multi-valued field through either parameter.
func TestFieldValue_MultiValueSerializesAsList(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values []string
		params sfParams
		want   string
	}{
		{
			name:   "bs wraps each line as its own byte sequence",
			values: []string{"one", "two"},
			params: sfParams{list: []sfParam{{key: "bs"}}},
			want:   ":b25l:, :dHdv:",
		},
		{
			name:   "sf re-serializes a list field comma-separated",
			values: []string{"1, 2"},
			params: sfParams{list: []sfParam{{key: "sf"}}},
			want:   "1, 2",
		},
		{
			name:   "sf across two field lines stays one list",
			values: []string{"1", "2"},
			params: sfParams{list: []sfParam{{key: "sf"}}},
			want:   "1, 2",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := http.Header{}
			for _, v := range tc.values {
				h.Add("X-Example", v)
			}
			got, err := fieldValue(h, "x-example", tc.params)
			if err != nil {
				t.Fatalf("fieldValue: %v", err)
			}
			if got != tc.want {
				t.Errorf("fieldValue = %q, want %q", got, tc.want)
			}
			if len(got) > 0 && got[0] == '(' {
				t.Errorf("serialized as an Inner List (%q); a List is comma-separated", got)
			}
		})
	}
}
