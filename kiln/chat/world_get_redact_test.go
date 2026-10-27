package chat_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/kiln/journal"
)

// world_get hands the world to the caller over the HTTP tool API, MCP,
// and ACP. GET /kiln/world and /kiln/status redact the credentials it
// carries; world_get returned them verbatim, so the same jwt_secret,
// seed_password and credentialed db_url were one tool call away. The
// in-process world keeps the real values: freeze reads those.
func TestWorldGetRedactsSecrets(t *testing.T) {
	l, _ := setup(t)
	const (
		jwt  = "jwt-canary-0123456789"             // nosecret: test fixture
		seed = "seed-canary-0123456789"            // nosecret: test fixture
		dsn  = "postgres://app:dsn-canary-0123@db" // nosecret: test fixture
	)
	if res := toolPost(t, l, "set_app_config", `{"config":{"name":"sec","db_url":"`+dsn+`",`+
		`"auth":{"enabled":true,"dev_mode":true,"jwt_secret":"`+jwt+`"},`+
		`"admin":{"enabled":true,"seed_email":"root@example.test","seed_password":"`+seed+`"}}}`); !res.OK {
		t.Fatalf("set_app_config: %+v", res)
	}
	for _, path := range []string{`{}`, `{"path":""}`} {
		res := toolPost(t, l, "world_get", path)
		if !res.OK {
			t.Fatalf("world_get %s: %+v", path, res)
		}
		body, err := json.Marshal(res.Result)
		if err != nil {
			t.Fatal(err)
		}
		for _, canary := range []string{jwt, seed, "dsn-canary-0123"} {
			if strings.Contains(string(body), canary) {
				t.Errorf("SECURITY: world_get %s returned %q verbatim: %.400s", path, canary, body)
			}
		}
		if !strings.Contains(string(body), "root@example.test") {
			t.Errorf("redaction removed non-secret config too: %.400s", body)
		}
	}
	l.ReadSession(func(sess *journal.Session) {
		app := sess.World.App
		if app.Auth.JWTSecret != jwt || app.Admin.SeedPassword != seed || app.DBURL != dsn {
			t.Errorf("redaction reached the in-process world: %+v", app)
		}
	})
}
