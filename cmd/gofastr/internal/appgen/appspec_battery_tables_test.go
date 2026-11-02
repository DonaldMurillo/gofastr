package appgen

import (
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/framework"
)

// TestAppSpecRefusesEntityOnBatteryTable ties the validator's battery-table
// list to the tables the emitted app actually hands its batteries. An
// entity named auth_users was migrated onto the auth battery's accounts
// and served them through its CRUD routes to every signed-in user; one
// named auth_sessions broke every session read. If the generator renames
// a battery table, this fails until the validator learns the new name.
func TestAppSpecRefusesEntityOnBatteryTable(t *testing.T) {
	bp := appspec.AppSpec{
		App: appspec.AppSpecApp{
			Name: "Tables", Module: "example.com/tables",
			Auth:  appspec.AppSpecAuth{Enabled: true, DevMode: true},
			Admin: appspec.AppSpecAdmin{Enabled: true, Path: "/admin", Role: "admin", LoginPath: "/login", SeedEmail: "admin@example.com", SeedPassword: "roster-pw-123"},
		},
		Entities: []framework.EntityDeclaration{{Name: "posts", Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}}}},
	}
	src := RenderAppSpecApp(bp) + RenderAppSpecMain(bp)
	var tables []string
	for _, re := range []*regexp.Regexp{
		regexp.MustCompile(`NewEntityUserStore\(db, "([A-Za-z_]+)"\)`),
		regexp.MustCompile(`NewEntitySessionStore\(db, "([A-Za-z_]+)"\)`),
		regexp.MustCompile(`AuditTable: "([A-Za-z_]+)"`),
	} {
		m := re.FindStringSubmatch(src)
		if m == nil {
			t.Fatalf("the emitted app no longer matches %s; update this test to the new spelling", re)
		}
		tables = append(tables, m[1])
		if strings.Contains(re.String(), "UserStore") {
			tables = append(tables, m[1]+"_oauth_links") // battery/auth's link table
		}
	}
	for _, table := range tables {
		spec := bp
		spec.Entities = append(spec.Entities, framework.EntityDeclaration{Name: "mine", Table: table, Fields: []framework.FieldDeclaration{{Name: "note", Type: "string"}}})
		err := appspec.ValidateAppSpec(spec)
		if err == nil || !strings.Contains(err.Error(), `uses the table "`+table+`"`) {
			t.Errorf("an entity on the battery table %q is not refused: %v", table, err)
		}
	}
}
