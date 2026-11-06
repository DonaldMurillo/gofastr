package packcmd

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
)

func TestPackOmitsImplicitAdminDefaults(t *testing.T) {
	for _, customized := range []bool{false, true} {
		admin := appspec.AppSpecAdmin{Enabled: true}
		if customized {
			admin.Path, admin.Role = "/backoffice", "operator"
		}
		spec := appspec.AppSpec{App: appspec.AppSpecApp{
			Name: "Admin", Module: "example.com/admin", DBDriver: "sqlite", DBURL: "file:admin.db", Admin: admin,
		}}
		packed, err := packAppSpec(materializeAppSpec(t, spec))
		if err != nil {
			t.Fatal(err)
		}
		if !packed.App.Admin.Enabled || packed.App.Admin.Path != admin.Path || packed.App.Admin.Role != admin.Role {
			t.Errorf("custom=%v: admin=%+v, want %+v", customized, packed.App.Admin, admin)
		}
	}
}
