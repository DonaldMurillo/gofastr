package filter

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// TestParseWhereDecodesStrictly pins ?where= to the no-ambiguity rule
// every other client-borne JSON body follows (pagination.DecodeCursor,
// handler.UnmarshalStrict). Plain json.Unmarshal resolved duplicate and
// case-folded keys last-wins and dropped unknown ones, so the tree that
// ran was not the tree a first-read proxy saw, and a typo'd member
// silently compared against the empty string. A leaf may carry only the value
// member its op reads.
func TestParseWhereDecodesStrictly(t *testing.T) {
	fields := []schema.Field{{Name: "status", Type: schema.String}}
	for _, raw := range []string{
		`{"field":"status","value":"a","value":"b"}`,                // duplicate key
		`{"field":"status","value":"a","VALUE":"b"}`,                // case-folded duplicate
		`{"field":"status","value":"a","ſtatus":"b","Status":"c"}`,  // unknown members
		`{"field":"status","vaule":"x"}`,                            // typo'd member
		`{"field":"status","op":"eq","values":["a","b"]}`,           // eq does not read values
		`{"field":"status","op":"in","value":"a","values":["b"]}`,   // in reads one of them
		`{"Field":"status","Value":"a"}`,                            // case-folded member names
		`{"or":[{"field":"status","value":"a","value":"b"}]}`,       // duplicate one level down
		`{"and":[{"field":"status","value":"a"}],"op":"eq"}`,        // group with a leaf member
		`{"and":[{"field":"status","value":"a"}],"field":"status"}`, // two shapes
		`{"and":[],"field":"status","value":"a"}`,                   // empty group member sent
		`{"or":[]}`, // empty group
		`{"field":"status","value":"a"} {"field":"status","value":"b"}`, // trailing value
	} {
		if p, err := ParseWhere(raw, fields); err == nil {
			t.Errorf("ParseWhere(%s) accepted: %+v", raw, *p)
		}
	}
	// The documented shapes still parse.
	for _, raw := range []string{
		`{"field":"status","value":"a"}`,
		`{"field":"status","op":"ne","value":"a"}`,
		`{"field":"status","op":"in","values":["a","b"]}`,
		`{"field":"status","op":"in","value":"a,b"}`,
		`{"or":[{"field":"status","value":"a"},{"and":[{"field":"status","op":"like","value":"b"}]}]}`,
	} {
		if _, err := ParseWhere(raw, fields); err != nil {
			t.Errorf("ParseWhere(%s): %v", raw, err)
		}
	}
}

// TestWhereRefusesNullValue: encoding/json reads a JSON null into a
// *string as "member not sent", so {"field":"title","value":null}
// compared title with ” on text columns (200) while an int column
// refused it. A leaf whose op reads a value must carry one, and an in
// list may not hold a null entry, on every column type.
func TestWhereRefusesNullValue(t *testing.T) {
	fields := []schema.Field{
		{Name: "title", Type: schema.String},
		{Name: "score", Type: schema.Int},
	}
	for _, f := range []string{"title", "score"} {
		for _, raw := range []string{
			`{"field":"` + f + `","value":null}`,
			`{"field":"` + f + `"}`,
			`{"field":"` + f + `","op":"ne"}`,
			`{"field":"` + f + `","op":"lt","value":null}`,
			`{"field":"` + f + `","op":"in","values":null}`,
			`{"field":"` + f + `","op":"in","values":[null]}`,
			`{"field":"` + f + `","op":"in","values":["1",null]}`,
		} {
			if p, err := ParseWhere(raw, fields); err == nil {
				t.Errorf("ParseWhere(%s) accepted: %+v", raw, *p)
			}
		}
	}
	if _, err := ParseWhere(`{"field":"title","op":"like","value":null}`, fields); err == nil {
		t.Error("like with a null value accepted")
	}
	// An empty string sent on purpose still compares with ''.
	if _, err := ParseWhere(`{"field":"title","value":""}`, fields); err != nil {
		t.Errorf("explicit empty value refused: %v", err)
	}
}
