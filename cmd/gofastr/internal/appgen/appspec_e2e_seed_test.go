package appgen

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
)

func TestE2ELoadsTheSeedPasswordWithoutGatedScreens(t *testing.T) {
	bp := appspec.AppSpec{
		App:     appspec.AppSpecApp{Name: "Demo", Module: "example.com/demo", Auth: appspec.AppSpecAuth{Enabled: true}, Admin: appspec.AppSpecAdmin{SeedEmail: "a@example.com", SeedPassword: "x"}},
		Screens: []appspec.AppSpecScreen{{Name: "home", Route: "/"}},
	}
	if src := renderAppSpecE2ETest(bp); !strings.Contains(src, "dotenv.LoadAndApply") {
		t.Fatalf("an app that seeds an admin needs ADMIN_SEED_PASSWORD to boot, so its e2e loads .env even with no gated screen:\n%s", src)
	}
}
