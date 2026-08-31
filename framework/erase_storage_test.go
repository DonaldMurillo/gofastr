package framework

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/core/upload"
	"github.com/DonaldMurillo/gofastr/framework/datexport"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// eraseProfilesEntity registers a profiles entity carrying an Image field
// plus its sibling renditions column, the shape crud's applyDerivedColumns
// writes.
func eraseProfilesEntity(t *testing.T, app *App) {
	t.Helper()
	app.Registry.Register(entity.Define("profiles", entity.EntityConfig{
		Table: "profiles",
		Scope: &entity.ScopeConfig{OwnerField: "owner_id"},
		Fields: []schema.Field{
			{Name: "avatar", Type: schema.Image},
			{Name: "avatar_variants", Type: schema.JSON},
			{Name: "owner_id", Type: schema.String},
		},
	}))
	if err := AutoMigrate(app.DB, app.Registry); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	if err := EnsureAuditTable(app.DB, ""); err != nil {
		t.Fatalf("EnsureAuditTable: %v", err)
	}
}

// TestEraseUserData_RemovesRenditions: the sibling <field>_variants column
// holds every stored rendition, and each is as much the user's file as the
// original. Erasure has to read that column, not just the primary.
func TestEraseUserData_RemovesRenditions(t *testing.T) {
	datexport.Reset(t)
	db := openSQLiteMem(t)
	store := &recordingDeleteStore{present: map[string]bool{}}
	app := NewApp(WithDB(db), WithFileStorage(store))
	eraseProfilesEntity(t, app)

	const primary = "uploads/profiles/avatar/a.png"
	const small = "uploads/profiles/avatar/a.png_sm.webp"
	const large = "uploads/profiles/avatar/a.png_lg.webp"
	for _, k := range []string{primary, small, large} {
		store.present[k] = true
	}
	if _, err := db.Exec(
		`INSERT INTO profiles (id, avatar, avatar_variants, owner_id, created_at, updated_at)
		 VALUES ('p1', ?, ?, 'u1', '2024-01-01T00:00:00Z', '2024-01-01T00:00:00Z')`,
		primary,
		`[{"storage_ref":"`+small+`"},{"storage_ref":"`+large+`"}]`,
	); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if _, err := app.EraseUserData(context.Background(), "u1"); err != nil {
		t.Fatalf("EraseUserData: %v", err)
	}
	for _, k := range []string{primary, small, large} {
		if store.present[k] {
			t.Errorf("object %q survived erasure", k)
		}
	}
}

// A malformed variants column is left alone rather than guessed at: it is
// not a rendition list, and erasure must not fail on one bad row.
func TestEraseUserData_TolerantOfUnparsableVariants(t *testing.T) {
	datexport.Reset(t)
	db := openSQLiteMem(t)
	store := &recordingDeleteStore{present: map[string]bool{}}
	app := NewApp(WithDB(db), WithFileStorage(store))
	eraseProfilesEntity(t, app)

	const primary = "uploads/profiles/avatar/b.png"
	store.present[primary] = true
	if _, err := db.Exec(
		`INSERT INTO profiles (id, avatar, avatar_variants, owner_id, created_at, updated_at)
		 VALUES ('p1', ?, 'not json at all', 'u1', '2024-01-01T00:00:00Z', '2024-01-01T00:00:00Z')`,
		primary,
	); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if _, err := app.EraseUserData(context.Background(), "u1"); err != nil {
		t.Fatalf("EraseUserData: %v", err)
	}
	if store.present[primary] {
		t.Error("the primary object survived because a sibling column did not parse")
	}
}

// The rows are committed by the time objects are deleted, so a storage
// failure cannot be swallowed: the caller needs the surviving keys named
// to finish the job by hand.
func TestEraseUserData_ReportsSurvivingObjects(t *testing.T) {
	datexport.Reset(t)
	db := openSQLiteMem(t)
	const primary = "uploads/profiles/avatar/c.png"
	store := &recordingDeleteStore{
		present: map[string]bool{primary: true},
		failOn:  primary,
	}
	app := NewApp(WithDB(db), WithFileStorage(store))
	eraseProfilesEntity(t, app)

	if _, err := db.Exec(
		`INSERT INTO profiles (id, avatar, avatar_variants, owner_id, created_at, updated_at)
		 VALUES ('p1', ?, '[]', 'u1', '2024-01-01T00:00:00Z', '2024-01-01T00:00:00Z')`,
		primary,
	); err != nil {
		t.Fatalf("seed: %v", err)
	}

	_, err := app.EraseUserData(context.Background(), "u1")
	if err == nil {
		t.Fatal("a storage delete failure was swallowed; the object is still reachable")
	}
	if !strings.Contains(err.Error(), primary) {
		t.Errorf("error does not name the surviving key: %v", err)
	}
	// The rows are gone regardless: the failure is in the object plane.
	if n := countWhere(t, db, "profiles", "owner_id", "u1"); n != 0 {
		t.Errorf("rows survived: %d", n)
	}
}

// An app with no Storage configured has nothing to delete and must not
// complain about it.
func TestEraseUserData_NoStorageConfigured(t *testing.T) {
	datexport.Reset(t)
	db := openSQLiteMem(t)
	app := NewApp(WithDB(db))
	eraseProfilesEntity(t, app)

	if _, err := db.Exec(
		`INSERT INTO profiles (id, avatar, avatar_variants, owner_id, created_at, updated_at)
		 VALUES ('p1', 'k', '[]', 'u1', '2024-01-01T00:00:00Z', '2024-01-01T00:00:00Z')`,
	); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := app.EraseUserData(context.Background(), "u1"); err != nil {
		t.Fatalf("EraseUserData with no Storage: %v", err)
	}
}

// recordingDeleteStore tracks which keys still exist and can be made to
// fail one delete.
type recordingDeleteStore struct {
	present map[string]bool
	failOn  string
}

func (s *recordingDeleteStore) Save(_ context.Context, key string, _ io.Reader) error {
	s.present[key] = true
	return nil
}

func (s *recordingDeleteStore) Delete(_ context.Context, key string) error {
	if key == s.failOn {
		return errors.New("storage unavailable")
	}
	delete(s.present, key)
	return nil
}

func (s *recordingDeleteStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	if !s.present[key] {
		return nil, upload.ErrNotFound
	}
	return io.NopCloser(strings.NewReader("")), nil
}

func (s *recordingDeleteStore) Exists(_ context.Context, key string) (bool, error) {
	return s.present[key], nil
}
