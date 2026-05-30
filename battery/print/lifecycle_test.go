package print

import (
	"net/http"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// TestInitMountsViaFramework exercises the real battery lifecycle path
// (Name + Init through framework.App), which the website demo skips by
// calling RegisterRoutes directly.
func TestInitMountsViaFramework(t *testing.T) {
	b := New(Config{DefaultAccess: Public}).Document(Document{
		Name: "d", Path: "/d", Build: docBuild("<p>ok</p>"),
	})
	if b.Name() != "print" {
		t.Fatalf("Name() = %q, want print", b.Name())
	}

	app := framework.NewApp()
	if err := b.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if rec := get(t, app.Router(), "/print/d"); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestPageConstructors(t *testing.T) {
	if p := A4Portrait(MM(10)); p.Size != A4 || p.Orientation != Portrait || p.Margin.Top != "10mm" {
		t.Errorf("A4Portrait = %+v", p)
	}
	if p := LetterPortrait(MM(8)); p.Size != Letter || p.Orientation != Portrait {
		t.Errorf("LetterPortrait = %+v", p)
	}
}
