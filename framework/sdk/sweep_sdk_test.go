package sdk

import (
	"archive/zip"
	"bytes"
	"testing"
	"testing/fstest"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func sweepHash(fields []schema.Field, search []string, eps []entity.Endpoint) string {
	return hashOf([]NamedConfig{{Name: "posts", Config: entity.EntityConfig{
		Fields: fields, SearchFields: search, Endpoints: eps,
	}}})
}

func TestReadManifestRefusesEachMissingPart(t *testing.T) {
	for name, bad := range map[string]string{
		"no version":  `{"entities":["posts"],"artifacts":{"go":{"file":"f","sha256":"ab"}}}`,
		"no artifact": `{"schemaVersion":2,"entities":["posts"],"artifacts":{}}`,
		"no sha":      `{"schemaVersion":2,"entities":["posts"],"artifacts":{"go":{"file":"f"}}}`,
		"no file":     `{"schemaVersion":2,"entities":["posts"],"artifacts":{"go":{"sha256":"ab"}}}`,
	} {
		if _, err := ReadManifest(fstest.MapFS{ManifestFile: {Data: []byte(bad)}}); err == nil {
			t.Errorf("%s manifest was accepted", name)
		}
	}
}

func TestPackZipSortsAndFixesMode(t *testing.T) {
	raw, err := PackZip("", []File{{Path: "b.go", Data: []byte("b")}, {Path: "a.go", Data: []byte("a")}})
	if err != nil {
		t.Fatal(err)
	}
	r, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	if r.File[0].Name != "a.go" {
		t.Errorf("entries must be in sorted order, first is %q", r.File[0].Name)
	}
	for _, f := range r.File {
		if f.Mode().Perm() != 0o644 {
			t.Errorf("%s: mode %v, want 0644", f.Name, f.Mode())
		}
	}
}

func TestSchemaHashKeepsEveryDivergentVersion(t *testing.T) {
	v1 := NamedConfig{Name: "posts", Config: entity.EntityConfig{Fields: []schema.Field{{Name: "title", Type: schema.String}}}}
	v2 := NamedConfig{Name: "posts", Config: entity.EntityConfig{Fields: []schema.Field{{Name: "body", Type: schema.Text}}}}
	both := hashOf([]NamedConfig{v1, v2})
	if both == hashOf([]NamedConfig{v1}) || both == hashOf([]NamedConfig{v2}) {
		t.Fatal("two divergent versions must both reach the hash")
	}
}

func TestSchemaHashCoversWireSurface(t *testing.T) {
	title := schema.Field{Name: "title", Type: schema.String}
	body := schema.Field{Name: "body", Type: schema.Text}
	sdkEP := entity.Endpoint{Name: "publish", Method: "POST", Path: "publish", SDK: true}
	base := sweepHash([]schema.Field{title, body}, []string{"title", "body"}, []entity.Endpoint{sdkEP})

	renamed := title
	renamed.WireName = "headline"
	moved := sdkEP
	moved.Path = "release"
	for name, h := range map[string]string{
		"wire name":     sweepHash([]schema.Field{renamed, body}, []string{"title", "body"}, []entity.Endpoint{sdkEP}),
		"search fields": sweepHash([]schema.Field{title, body}, []string{"title"}, []entity.Endpoint{sdkEP}),
		"sdk endpoint":  sweepHash([]schema.Field{title, body}, []string{"title", "body"}, []entity.Endpoint{moved}),
	} {
		if h == base {
			t.Errorf("a changed %s must move the hash", name)
		}
	}

	handOnly := entity.Endpoint{Name: "ping", Method: "GET", Path: "ping"}
	for name, h := range map[string]string{
		"field order":           sweepHash([]schema.Field{body, title}, []string{"title", "body"}, []entity.Endpoint{sdkEP}),
		"search order":          sweepHash([]schema.Field{title, body}, []string{"body", "title"}, []entity.Endpoint{sdkEP}),
		"hand-written endpoint": sweepHash([]schema.Field{title, body}, []string{"title", "body"}, []entity.Endpoint{sdkEP, handOnly}),
	} {
		if h != base {
			t.Errorf("%s must not move the hash", name)
		}
	}
}
