package sdk

import (
	"archive/zip"
	"bytes"
	"testing"
	"testing/fstest"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func postsConfig() entity.EntityConfig {
	return entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "views", Type: schema.Int},
			{Name: "secret", Type: schema.String, Hidden: true},
		},
		Public: true,
	}
}

func TestSchemaHashDeterministic(t *testing.T) {
	a := SchemaHash([]NamedConfig{{Name: "posts", Config: postsConfig()}})
	b := SchemaHash([]NamedConfig{{Name: "posts", Config: postsConfig()}})
	if a != b {
		t.Fatalf("same config hashed differently: %s vs %s", a, b)
	}
	if len(a) == 0 || a[:7] != "sha256:" {
		t.Fatalf("unexpected hash format %q", a)
	}
}

func TestSchemaHashChangesOnFieldType(t *testing.T) {
	base := SchemaHash([]NamedConfig{{Name: "posts", Config: postsConfig()}})
	changed := postsConfig()
	changed.Fields[1].Type = schema.Float
	if got := SchemaHash([]NamedConfig{{Name: "posts", Config: changed}}); got == base {
		t.Fatal("field type change did not change the hash")
	}
}

func TestSchemaHashIgnoresHiddenFields(t *testing.T) {
	base := SchemaHash([]NamedConfig{{Name: "posts", Config: postsConfig()}})
	changed := postsConfig()
	changed.Fields[2].Type = schema.Text // still Hidden
	if got := SchemaHash([]NamedConfig{{Name: "posts", Config: changed}}); got != base {
		t.Fatal("hidden field change leaked into the hash")
	}
}

func TestSchemaHashMatchesDefinedEntity(t *testing.T) {
	// The generation side hashes raw declaration configs; the serving side
	// hashes registry entities that already went through Define (injected
	// id/created_at/updated_at, defaulted Table). Both must agree.
	raw := SchemaHash([]NamedConfig{{Name: "posts", Config: postsConfig()}})
	defined := entity.Define("posts", postsConfig())
	viaRegistry := SchemaHash([]NamedConfig{{Name: defined.Config.Name, Config: defined.Config}})
	if raw != viaRegistry {
		t.Fatalf("raw config and Defined entity hash differently: %s vs %s", raw, viaRegistry)
	}
}

func TestSchemaHashEntityOrderIrrelevant(t *testing.T) {
	users := entity.EntityConfig{Fields: []schema.Field{{Name: "email", Type: schema.String}}}
	ab := SchemaHash([]NamedConfig{{Name: "posts", Config: postsConfig()}, {Name: "users", Config: users}})
	ba := SchemaHash([]NamedConfig{{Name: "users", Config: users}, {Name: "posts", Config: postsConfig()}})
	if ab != ba {
		t.Fatal("entity order changed the hash")
	}
}

func TestPackZipDeterministic(t *testing.T) {
	files := []File{
		{Path: "go.mod", Data: []byte("module local/x\n")},
		{Path: "client.go", Data: []byte("package client\n")},
	}
	a, err := PackZip("x-sdk", files)
	if err != nil {
		t.Fatal(err)
	}
	// Reversed input order must not matter.
	b, err := PackZip("x-sdk", []File{files[1], files[0]})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("two packs of the same files differ")
	}
}

func TestPackZipPrefixesEntries(t *testing.T) {
	raw, err := PackZip("myapp-sdk", []File{{Path: "go.mod", Data: []byte("module m\n")}})
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	if len(zr.File) != 1 || zr.File[0].Name != "myapp-sdk/go.mod" {
		t.Fatalf("unexpected entries: %+v", zr.File)
	}
	// A zero Modified round-trips through the DOS timestamp as the 1979/80
	// epoch — anything later means a real mtime leaked in.
	if zr.File[0].Modified.Year() > 1980 {
		t.Fatalf("entry carries a real timestamp (%v); archives must be deterministic", zr.File[0].Modified)
	}
}

func TestPackZipRejectsUnsafePaths(t *testing.T) {
	for _, p := range []string{"", "/abs", "../escape"} {
		if _, err := PackZip("p", []File{{Path: p}}); err == nil {
			t.Fatalf("path %q was accepted", p)
		}
	}
}

func TestReadManifestValidates(t *testing.T) {
	good := `{"schemaVersion":1,"app":"x","artifacts":{"go":{"file":"sdk-go.zip","sha256":"ab","bytes":2}}}`
	m, err := ReadManifest(fstest.MapFS{ManifestFile: {Data: []byte(good)}})
	if err != nil {
		t.Fatal(err)
	}
	if m.Artifacts["go"].File != GoArtifact {
		t.Fatalf("unexpected manifest: %+v", m)
	}

	for name, bad := range map[string]string{
		"malformed":   `{`,
		"no version":  `{"artifacts":{"go":{"file":"f","sha256":"ab"}}}`,
		"no artifact": `{"schemaVersion":1,"artifacts":{}}`,
		"no sha":      `{"schemaVersion":1,"artifacts":{"go":{"file":"f"}}}`,
	} {
		if _, err := ReadManifest(fstest.MapFS{ManifestFile: {Data: []byte(bad)}}); err == nil {
			t.Fatalf("%s manifest was accepted", name)
		}
	}
	if _, err := ReadManifest(fstest.MapFS{}); err == nil {
		t.Fatal("missing manifest was accepted")
	}
}

func TestRegistryNamedConfigsSkipsUnknown(t *testing.T) {
	reg := &fakeRegistry{entities: map[string]*entity.Entity{
		"posts": entity.Define("posts", postsConfig()),
	}}
	named := RegistryNamedConfigs(reg, []string{"posts", "gone"})
	if len(named) != 1 || named[0].Name != "posts" {
		t.Fatalf("unexpected configs: %+v", named)
	}
}

type fakeRegistry struct{ entities map[string]*entity.Entity }

func (f *fakeRegistry) All() map[string]*entity.Entity { return f.entities }
func (f *fakeRegistry) AllSorted() []*entity.Entity {
	var out []*entity.Entity
	for _, e := range f.entities {
		out = append(out, e)
	}
	return out
}
func (f *fakeRegistry) Get(name string) (*entity.Entity, error) {
	e, ok := f.entities[name]
	if !ok {
		return nil, errNotFound
	}
	return e, nil
}

var errNotFound = &notFoundError{}

type notFoundError struct{}

func (*notFoundError) Error() string { return "not found" }
