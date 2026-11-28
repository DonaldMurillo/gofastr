package render

import (
	"slices"
	"testing"
)

func TestWrittenIDsAfterLiteralLessThan(t *testing.T) {
	got := writtenIDs(`<p>2 < 3 <b id="price">price</b></p>`)
	if !slices.Equal(got, []string{"price"}) {
		t.Fatalf("IDs = %v", got)
	}
}

func TestWrittenIDsUsesFirstAttribute(t *testing.T) {
	got := writtenIDs(`<p ID="first" id="ignored"></p><p id="" id="alsoIgnored"></p>`)
	if !slices.Equal(got, []string{"first"}) {
		t.Fatalf("IDs = %v", got)
	}
}

func TestWrittenIDsIgnoresUnfinishedTag(t *testing.T) {
	got := writtenIDs(`<p id="unfinished"`)
	if len(got) != 0 {
		t.Fatalf("IDs = %v", got)
	}
}

func TestRegionSnapshotsIDFamily(t *testing.T) {
	ctx := WithIDScope(t.Context())
	batch := NewRegionBatch(ctx, 1)
	children := []string{"-label"}
	id := UniqueID(batch.Context(0), "card", children...)
	children[0] = "-wrong"
	batch.Settle(0)(HTML(id))
	if got := UniqueID(ctx, "card-label"); got != "card-label-2" {
		t.Fatalf("ID = %q, want card-label-2", got)
	}
}
