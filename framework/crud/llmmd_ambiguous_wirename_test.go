package crud

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// llm.md gains one line per bare filter key that reads two ways and
// answers 400 (stage beside stage_in), naming the bracket spellings that
// pick a reading. A field that pins its wire key to its own column name
// (WireName: "stage_in", how a camelCase app keeps one snake_case key)
// collides the same way, the route refuses ?stage_in= for it, and the
// page says so once.
//
// The bug this holds: the check that kept a WireName repeating the Name
// from being written twice skipped both spellings, so the page said
// nothing.
func TestLLMMDNamesAmbiguousKeyWithPinnedWireName(t *testing.T) {
	const want = "`?stage_in=` reads two ways and answers 400: send `?stage_in[eq]=`"

	control := EntityLLMMD(mkEntity("stages", "", []schema.Field{
		{Name: "stage", Type: schema.String},
		{Name: "stage_in", Type: schema.String},
	}))
	if !strings.Contains(control, want) {
		t.Fatalf("control (no WireName) lost the ambiguity line; the test proves nothing")
	}

	pinned := EntityLLMMD(mkEntity("stages", "", []schema.Field{
		{Name: "stage", Type: schema.String},
		{Name: "stage_in", Type: schema.String, WireName: "stage_in"},
	}))
	if n := strings.Count(pinned, "`?stage_in=` reads two ways"); n != 1 || !strings.Contains(pinned, want) {
		t.Errorf("a field that sets WireName to its own name: the ambiguity line is written %d times, want once:\n%s", n, pinned)
	}

	// A wire name that is another spelling is its own key, and each key
	// that collides gets its line.
	renamed := EntityLLMMD(mkEntity("stages", "", []schema.Field{
		{Name: "stage", Type: schema.String},
		{Name: "phase", Type: schema.String},
		{Name: "stage_in", Type: schema.String, WireName: "phase_in"},
	}))
	for _, key := range []string{"stage_in", "phase_in"} {
		if n := strings.Count(renamed, "`?"+key+"=` reads two ways"); n != 1 {
			t.Errorf("column stage_in under wire name phase_in: the %s line is written %d times, want once", key, n)
		}
	}
}
