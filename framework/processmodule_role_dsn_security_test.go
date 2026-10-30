package framework

import (
	"net/url"
	"testing"
)

// The module-role DSN keeps only where the admin DSN's server is and how
// to reach it. A user, password, passfile or service in its query names
// a login, and lib/pq sorts its key=value pairs, so the admin's can
// outrank the role's; lib/pq also sends a key it does not know as a
// startup setting, so options or search_path would undo the search_path
// provisioning set on the role.
func TestModuleRoleDSNDropsAdminParams(t *testing.T) {
	admin := "postgres://admin:adminpw@db.internal:5432/app" + // not-a-secret: test fixture
		"?sslmode=verify-full&sslrootcert=/ca.pem&connect_timeout=5&application_name=host" +
		"&user=postgres&password=hunter2&passfile=/pgpass&service=prod" + // not-a-secret: test fixture
		"&options=-c%20search_path%3Dpublic&search_path=public&role=postgres"
	got, err := moduleRoleDSN(admin, "module_x_role", "rolepw")
	if err != nil {
		t.Fatalf("moduleRoleDSN: %v", err)
	}
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse %q: %v", got, err)
	}
	if pw, _ := u.User.Password(); u.User.Username() != "module_x_role" || pw != "rolepw" {
		t.Errorf("userinfo = %v, want module_x_role:rolepw", u.User)
	}
	if u.Host != "db.internal:5432" || u.Path != "/app" {
		t.Errorf("host/db = %s%s, want db.internal:5432/app", u.Host, u.Path)
	}
	q := u.Query()
	for _, k := range []string{"user", "password", "passfile", "service", "options", "search_path", "role"} {
		if q.Has(k) {
			t.Errorf("role DSN keeps %s=%q from the admin DSN", k, q.Get(k))
		}
	}
	for k, want := range map[string]string{"sslmode": "verify-full", "sslrootcert": "/ca.pem", "connect_timeout": "5", "application_name": "host"} {
		if q.Get(k) != want {
			t.Errorf("role DSN %s = %q, want %q", k, q.Get(k), want)
		}
	}
}
