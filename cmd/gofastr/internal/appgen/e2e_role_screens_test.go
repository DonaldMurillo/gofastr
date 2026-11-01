package appgen

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
)

// The seeded admin holds the admin role and "user". A screen requiring
// another role answers it 403, so the generated e2e must expect 403 there
// instead of failing a correct app; screens it can open still expect 200.
func TestE2EExpects403ForRoleAdminLacks(t *testing.T) {
	bp := authBaseSpec("")
	bp.Screens = append(bp.Screens,
		appspec.AppSpecScreen{Name: "editor", Route: "/editor", Layout: "app", Access: appspec.AppSpecAccess{Auth: true, Role: "editor"}, Body: []appspec.AppSpecBlock{{Kind: "text", Text: "ed"}}},
		appspec.AppSpecScreen{Name: "staff", Route: "/staff", Layout: "app", Access: appspec.AppSpecAccess{Auth: true, Role: "admin"}, Body: []appspec.AppSpecBlock{{Kind: "text", Text: "st"}}},
	)
	e2e := renderAppSpecE2ETest(bp)
	allowed := "for _, p := range []string{\"/dashboard\", \"/staff\"} {\n\t\tif code, body := e2eDo(t, client, \"GET\", base+p, \"\"); code != http.StatusOK {"
	if !strings.Contains(e2e, allowed) {
		t.Errorf("e2e does not expect 200 from exactly the screens the admin can open:\n%s", e2e)
	}
	locked := "for _, p := range []string{\"/editor\"} {\n\t\tif code, _ := e2eDo(t, client, \"GET\", base+p, \"\"); code != http.StatusForbidden {"
	if !strings.Contains(e2e, locked) {
		t.Errorf("e2e does not expect 403 from /editor:\n%s", e2e)
	}
}
