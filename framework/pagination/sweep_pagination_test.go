package pagination

import (
	"errors"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

func wantPaginationReason(t *testing.T, err error, reason i18n.Phrase) {
	t.Helper()
	var ie *i18n.Error
	if !errors.As(err, &ie) || ie.Reason != reason {
		t.Fatalf("error %v, want %s", err, reason.Key())
	}
}

func TestDecodersRefuseOversizeCursorFirst(t *testing.T) {
	huge := strings.Repeat("A", maxCursorEncodedSize+4)
	_, _, err := DecodeCursor(huge)
	wantPaginationReason(t, err, reasonCursorTooLong)
	_, err = DecodeMultiCursor(huge)
	wantPaginationReason(t, err, reasonCursorTooLong)
}

func TestMultiCursorFieldCapUnderSizeCap(t *testing.T) {
	fields := make([]string, maxCursorFields+1)
	row := map[string]any{}
	for i := range fields {
		fields[i] = "f" + strconv.Itoa(i)
		row[fields[i]] = 1
	}
	cursor := EncodeMultiCursor(fields, row)
	if len(cursor) > maxCursorEncodedSize {
		t.Fatalf("test cursor is %d bytes, over the size cap", len(cursor))
	}
	_, err := DecodeMultiCursor(cursor)
	wantPaginationReason(t, err, reasonCursorTooManyFields)
}

func TestParsePaginationClampsLimit(t *testing.T) {
	for raw, want := range map[string]int{"limit=1000": MaxPageSize, "limit=0": DefaultPageSize, "limit=x": DefaultPageSize, "": DefaultPageSize} {
		_, limit, _ := ParsePagination(httptest.NewRequest("GET", "/?"+raw, nil))
		if limit != want {
			t.Errorf("?%s: limit %d, want %d", raw, limit, want)
		}
		_, limit, _ = ParseCursorPagination(httptest.NewRequest("GET", "/?"+raw, nil))
		if limit != want {
			t.Errorf("cursor ?%s: limit %d, want %d", raw, limit, want)
		}
	}
}

func TestOffsetPageRoundsTotalPagesUp(t *testing.T) {
	if got := NewOffsetPage(nil, 1, 10, 21).TotalPages; got != 3 {
		t.Fatalf("21 rows at 10 a page is 3 pages, got %d", got)
	}
	if got := NewOffsetPage(nil, 1, 0, 21).TotalPages; got != 0 {
		t.Fatalf("a zero page size makes 0 pages, got %d", got)
	}
}
