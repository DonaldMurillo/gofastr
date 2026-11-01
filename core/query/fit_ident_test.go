package query

import (
	"strings"
	"testing"
)

// A name that fits keeps its spelling, so every index an older build
// created is still found by the name it was created under.
func TestFitIdentKeepsShortNames(t *testing.T) {
	for _, n := range []string{"idx_widgets_title", strings.Repeat("a", MaxIdentBytes)} {
		if got := FitIdent(n); got != n {
			t.Errorf("FitIdent(%q) = %q, want it unchanged", n, got)
		}
	}
}

// Two long names sharing their first 63 bytes stay two names, each within
// the limit; a case variant of one names the same object, as unquoted DDL
// folds it.
func TestFitIdentHashesLongNames(t *testing.T) {
	base := "idx_inventory_bins_warehouse_identifier_code_bin_location_identifier_"
	a, b := FitIdent(base+"a"), FitIdent(base+"b")
	if a == b {
		t.Fatalf("both names fit to %q", a)
	}
	for _, n := range []string{a, b} {
		if len(n) > MaxIdentBytes {
			t.Errorf("%q is %d bytes, over %d", n, len(n), MaxIdentBytes)
		}
		if !strings.HasPrefix(n, base[:MaxIdentBytes-9]+"_") {
			t.Errorf("%q does not keep the name's head", n)
		}
	}
	if got := FitIdent(strings.ToUpper(base) + "A"); !strings.EqualFold(got, a) {
		t.Errorf("case variant fits to %q, want %q folded", got, a)
	}
}

// A store's index name built from a long configured table fits after the
// schema is dropped, so a schema prefix never pushes a short name into
// the hashed form.
func TestQuoteIndexNameFitsBarePart(t *testing.T) {
	long := strings.Repeat("t", 60)
	a, b := QuoteIndexName(long+"_status_idx"), QuoteIndexName(long+"_claim_idx")
	if a == b {
		t.Fatalf("both index names quote to %s", a)
	}
	if len(a) > MaxIdentBytes+2 || len(b) > MaxIdentBytes+2 {
		t.Errorf("quoted names %s, %s are over %d bytes", a, b, MaxIdentBytes)
	}
	short := strings.Repeat("s", 40) + "." + strings.Repeat("t", 40) + "_idx"
	if got, want := QuoteIndexName(short), `"`+strings.Repeat("t", 40)+`_idx"`; got != want {
		t.Errorf("QuoteIndexName(%q) = %s, want %s", short, got, want)
	}
}

// ObjectName trims the longer part first, the way Postgres names an
// inline constraint, so short column names on a long table stay apart.
func TestObjectNameTrimsLongerPart(t *testing.T) {
	table := "t" + strings.Repeat("x", 57)
	for _, tc := range []struct{ col, want string }{
		{"email", table[:53] + "_email_key"},
		{"email_alt", table[:49] + "_email_alt_key"},
	} {
		if got := ObjectName(table, tc.col, "key"); got != tc.want {
			t.Errorf("ObjectName(%d bytes, %q) = %q, want %q", len(table), tc.col, got, tc.want)
		}
	}
	if got := ObjectName("widgets", "sku", "key"); got != "widgets_sku_key" {
		t.Errorf("short name = %q, want widgets_sku_key", got)
	}
}
