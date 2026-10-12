package framework

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/core/upload"
	"github.com/DonaldMurillo/gofastr/framework/datexport"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/file"
)

// EraseUserData deletes the stored objects the erased user's rows name. A
// row could name another user's object: before the CRUD write path checked
// provenance, any caller could copy a victim's key (published in the
// victim's /uploads/<key> URLs) into its own Image field or variants column,
// erase itself, and delete the victim's files. Rows written that way are
// still in existing databases, and host code with WithServerWrites can still
// write any key. So erasure deletes a key only when no surviving row in any
// entity still names it, and never deletes an absolute http(s) URL.

// deleteLog wraps a Storage and records every Delete key.
type deleteLog struct {
	upload.Storage
	mu   sync.Mutex
	keys []string
}

func (d *deleteLog) Delete(ctx context.Context, key string) error {
	d.mu.Lock()
	d.keys = append(d.keys, key)
	d.mu.Unlock()
	return d.Storage.Delete(ctx, key)
}

func TestEraseKeepsKeysOthersStillUse(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		eraseKeepsSharedKeys(t, db)
	})
}

func eraseKeepsSharedKeys(t *testing.T, db *sql.DB) {
	datexport.Reset(t)
	local := upload.NewLocalStorage(t.TempDir())
	store := &deleteLog{Storage: local}
	app := NewApp(WithDB(db), WithFileStorage(store))
	app.Registry.Register(entity.Define("profiles", entity.EntityConfig{
		Table: "profiles",
		Scope: &entity.ScopeConfig{OwnerField: "owner_id"},
		Fields: []schema.Field{
			{Name: "avatar", Type: schema.Image},
			{Name: "avatar_variants", Type: schema.JSON},
			{Name: "owner_id", Type: schema.String},
		},
	}))
	// Not owner-scoped: erasure never deletes its rows, so a key it names
	// must survive too.
	app.Registry.Register(entity.Define("posts", entity.EntityConfig{
		Table:  "posts",
		Fields: []schema.Field{{Name: "cover", Type: schema.Image}},
	}))
	if err := AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}

	ctx := context.Background()
	put := func(name string) string {
		ff, err := file.ProcessFileField(ctx, local, strings.NewReader(name), name+".png", "profiles", "avatar")
		if err != nil {
			t.Fatalf("ProcessFileField: %v", err)
		}
		return ff.StorageRef
	}
	victimAvatar, victimVariant, victimCover := put("v-avatar"), put("v-variant"), put("v-cover")
	ownAvatar, ownVariant := put("own-avatar"), put("own-variant")
	const external = "https://cdn.example.com/x.png"

	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("seed: %v\nquery: %s", err, q)
		}
	}
	ts := "'2024-01-01T00:00:00Z'"
	ins := `INSERT INTO profiles (id, avatar, avatar_variants, owner_id, created_at, updated_at) VALUES ($1, $2, $3, $4, ` + ts + `, ` + ts + `)`
	// The victim's row. Its variants are spaced the way Postgres renders
	// jsonb, not the way Go marshals it.
	exec(ins, "v", victimAvatar, `[{"storage_ref": "`+victimVariant+`", "width": 320}]`, "u2")
	exec(`INSERT INTO posts (id, cover, created_at, updated_at) VALUES ('c', $1, `+ts+`, `+ts+`)`, victimCover)
	// u1's rows, planted by SQL: two name the victim's objects.
	exec(ins, "p1", victimAvatar, `[{"storage_ref":"`+victimVariant+`"},{"storage_ref":"`+ownVariant+`"}]`, "u1")
	exec(ins, "p2", victimCover, nil, "u1")
	exec(ins, "p3", ownAvatar, nil, "u1")
	exec(ins, "p4", external, `[{"storage_ref":"`+external+`"}]`, "u1")

	if _, err := app.EraseUserData(ctx, "u1"); err != nil {
		t.Fatalf("EraseUserData: %v", err)
	}

	serve := func(key string) int {
		mux := http.NewServeMux()
		mux.HandleFunc("/uploads/{key...}", upload.ServeHandler(local))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/uploads/"+key, nil))
		return rec.Code
	}
	for name, k := range map[string]string{"victim avatar": victimAvatar, "victim variant": victimVariant, "victim post cover": victimCover} {
		if code := serve(k); code != http.StatusOK {
			t.Errorf("%s deleted by u1's erasure: GET = %d, want 200", name, code)
		}
	}
	for name, k := range map[string]string{"own avatar": ownAvatar, "own variant": ownVariant} {
		if code := serve(k); code != http.StatusNotFound {
			t.Errorf("%s survived u1's erasure: GET = %d, want 404", name, code)
		}
	}
	if slices.Contains(store.keys, external) {
		t.Errorf("erasure passed an external URL to Storage.Delete: %v", store.keys)
	}
}
