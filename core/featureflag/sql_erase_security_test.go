package featureflag_test

import (
	"bufio"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/featureflag"
	"github.com/DonaldMurillo/gofastr/framework"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// Erasing a user takes them out of every flag's user allow list and
// keeps the flags and the other members (S27). A data export carries
// the flag table under its configured name.
func TestEraseRemovesFlagCohortMember(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	s, err := featureflag.NewSQLStore(db, featureflag.WithSQLTable("rollouts"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []featureflag.Flag{
		{Key: "beta", Enabled: true, Users: []string{"victim", "bystander"}},
		{Key: "solo", Enabled: true, Users: []string{"victim"}},
	} {
		if err := s.Set(f); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	app := framework.NewApp(framework.WithDB(db))
	dir := t.TempDir()
	if err := app.ExportData(ctx, dir); err != nil {
		t.Fatalf("ExportData: %v", err)
	}
	f, err := os.Open(filepath.Join(dir, "feature_flags-rollouts.ndjson"))
	if err != nil {
		t.Errorf("the export has no feature_flags: %v", err)
	} else {
		n := 0
		for sc := bufio.NewScanner(f); sc.Scan(); {
			n++
		}
		_ = f.Close()
		if n != 2 {
			t.Errorf("export holds %d flag(s), want 2", n)
		}
	}
	if _, err := app.EraseUserData(ctx, "victim"); err != nil {
		t.Fatalf("EraseUserData: %v", err)
	}
	for key, want := range map[string][]string{"beta": {"bystander"}, "solo": nil} {
		got, err := s.Get(ctx, key)
		if err != nil || got == nil {
			t.Fatalf("Get %s = %v, %v; the flag itself must survive", key, got, err)
		}
		if !slices.Equal(got.Users, want) {
			t.Errorf("%s users = %v, want %v", key, got.Users, want)
		}
	}
}

// A user listed in a tenant's spelling ("acme:victim", ":victim") is
// erased too: the entry names the user whatever its tenant part. The
// other members stay, including tenant "victim"'s user "other", whose
// tenant part only looks like the erased id.
func TestEraseRemovesTenantQualifiedMember(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	s, err := featureflag.NewSQLStore(db, featureflag.WithSQLTable("tenant_rollouts"))
	if err != nil {
		t.Fatal(err)
	}
	users := []string{
		"acme:victim", "a%3Ab:victim", ":victim", "victim",
		"acme:bystander", "victim:other", "acme:victim2", "x:acme:victim",
	}
	if err := s.Set(featureflag.Flag{Key: "beta", Enabled: true, Users: users}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	app := framework.NewApp(framework.WithDB(db))
	if _, err := app.EraseUserData(ctx, "victim"); err != nil {
		t.Fatalf("EraseUserData: %v", err)
	}
	got, err := s.Get(ctx, "beta")
	if err != nil || got == nil {
		t.Fatalf("Get = %v, %v", got, err)
	}
	want := []string{"acme:bystander", "victim:other", "acme:victim2", "x:acme:victim"}
	if !slices.Equal(got.Users, want) {
		t.Errorf("SECURITY: [featureflag] users after erasure = %v, want %v. "+
			"Attack: a tenant-qualified allow-list entry keeps an erased user's id on file.", got.Users, want)
	}
}
