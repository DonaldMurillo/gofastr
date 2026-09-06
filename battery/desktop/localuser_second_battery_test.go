package desktop

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/owner"
)

// A second desktop battery in the same process must recognize the
// owner extractor the FIRST one installed as its own, not as
// battery/auth's. It did not: installOwnerExtractor took any installed
// extractor as proof that auth owned identity, so every app after the
// first in a test process ran its window anonymous (POST /api/notes
// from the window answered 401). Found by the desktoptest harness, whose
// second test per package was the second battery.
func TestSecondBatteryKeepsLocalIdentity(t *testing.T) {
	prev := owner.GetExtractor()
	t.Cleanup(func() { owner.SetExtractor(prev) })
	owner.SetExtractor(nil)

	b1, _ := newTestBattery(t)
	b1.installOwnerExtractor()
	if b1.authOwnsIdentity {
		t.Fatal("first battery: nothing else installed an extractor, yet identity was left to auth")
	}
	if owner.GetExtractor() == nil {
		t.Fatal("first battery installed no extractor")
	}

	b2, _ := newTestBattery(t)
	b2.installOwnerExtractor()
	if b2.authOwnsIdentity {
		t.Fatal("second battery mistook the desktop extractor for battery/auth's and disabled the local identity")
	}

	// A foreign extractor (battery/auth's, installed from its package
	// init) still wins: the local identity stays a fallback.
	owner.SetExtractor(func(context.Context) (any, bool) { return nil, false })
	b3, _ := newTestBattery(t)
	b3.installOwnerExtractor()
	if !b3.authOwnsIdentity {
		t.Fatal("a foreign extractor was replaced by the desktop battery")
	}
}
