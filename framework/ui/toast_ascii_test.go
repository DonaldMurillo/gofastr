package ui_test

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	ui "github.com/DonaldMurillo/gofastr/framework/ui"
)

// The Fetch API decodes header bytes as ISO-8859-1, so raw UTF-8 in
// X-Gofastr-Toast rendered "Creación" as "CreaciÃ³n". The header holds
// ASCII only, non-ASCII as JSON \u escapes that JSON.parse restores.
func TestToastHeaderIsASCII(t *testing.T) {
	rec := httptest.NewRecorder()
	ui.AddToastSuccess(rec, "Creación guardada", "日本語 😀", 0)
	ui.AddToastSuccess(rec, "Segundo ✓", "", 0)
	h := rec.Header().Get("X-Gofastr-Toast")
	for i := 0; i < len(h); i++ {
		if h[i] >= 0x80 {
			t.Fatalf("header byte %d is %#x, not ASCII: %q", i, h[i], h)
		}
	}
	var got []ui.ToastTrigger
	if err := json.Unmarshal([]byte(h), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Title != "Creación guardada" || got[0].Body != "日本語 😀" || got[1].Title != "Segundo ✓" {
		t.Fatalf("round trip = %+v", got)
	}
}

// The invalidation header takes the same encoding: a non-ASCII path
// prefix must reach the runtime as the path, not its Latin-1 reading.
func TestInvalidateHeaderIsASCII(t *testing.T) {
	rec := httptest.NewRecorder()
	ui.InvalidateScreens(rec, "/café")
	h := rec.Header().Get("X-Gofastr-Invalidate")
	for i := 0; i < len(h); i++ {
		if h[i] >= 0x80 {
			t.Fatalf("header byte %d is %#x, not ASCII: %q", i, h[i], h)
		}
	}
	var got []string
	if err := json.Unmarshal([]byte(h), &got); err != nil || len(got) != 1 || got[0] != "/café" {
		t.Fatalf("round trip = %v (%v)", got, err)
	}
}
