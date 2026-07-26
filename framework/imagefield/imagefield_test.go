package imagefield_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/file"
	fwimage "github.com/DonaldMurillo/gofastr/framework/image"
	"github.com/DonaldMurillo/gofastr/framework/imagefield"
)

// memStore is a minimal upload.Storage recording what got written.
type memStore struct {
	mu      sync.Mutex
	objects map[string][]byte
	failOn  string
}

func newMemStore() *memStore { return &memStore{objects: map[string][]byte{}} }

func (m *memStore) Save(_ context.Context, key string, r io.Reader) error {
	if m.failOn != "" && strings.Contains(key, m.failOn) {
		return io.ErrUnexpectedEOF
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[key] = data
	return nil
}

func (m *memStore) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objects, key)
	return nil
}

func (m *memStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.objects[key]
	if !ok {
		return nil, io.EOF
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (m *memStore) Exists(_ context.Context, key string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.objects[key]
	return ok, nil
}

func (m *memStore) keys() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.objects))
	for k := range m.objects {
		out = append(out, k)
	}
	return out
}

func testPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img, err := fwimage.NewGradient(w, h, "#4338CA", "#0E7C86")
	if err != nil {
		t.Fatalf("NewGradient: %v", err)
	}
	data, err := img.PNG().Bytes()
	if err != nil {
		t.Fatalf("PNG: %v", err)
	}
	return data
}

func landscapeConfig() imagefield.Config {
	return imagefield.Config{
		Variants: []fwimage.Variant{
			{Width: 320, Format: fwimage.FormatJPEG, Quality: 80, Suffix: "sm"},
			{Width: 640, Format: fwimage.FormatJPEG, Quality: 82, Suffix: "md"},
		},
		BlurHashX: 4, BlurHashY: 3,
	}
}

func TestDeriveImageProducesVariantsAndHash(t *testing.T) {
	d, err := imagefield.New(landscapeConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	store := newMemStore()
	got, err := d.DeriveImage(context.Background(), store, testPNG(t, 800, 600), "products/cover/a1b2-photo.png")
	if err != nil {
		t.Fatalf("DeriveImage: %v", err)
	}
	if len(got.Variants) != 2 {
		t.Fatalf("variants = %d, want 2: %+v", len(got.Variants), got.Variants)
	}
	// Ascending width so the slice drops straight into a srcset.
	if got.Variants[0].Width >= got.Variants[1].Width {
		t.Errorf("variants not ascending by width: %+v", got.Variants)
	}
	if got.BlurHash == "" {
		t.Error("no BlurHash produced")
	}
	// A 4x3 hash is 28 chars; assert the decoder accepts it rather than
	// hardcoding the length.
	if _, err := fwimage.BlurHashDataURL(got.BlurHash, fwimage.BlurHashRenderConfig{}); err != nil {
		t.Errorf("produced BlurHash does not decode: %v", err)
	}
	if got.Placeholder != "" {
		t.Errorf("Placeholder should be empty when not requested, got %.30q", got.Placeholder)
	}

	// Renditions must land beside the original, sharing its directory and
	// its random suffix, so two uploads of "photo.png" never collide.
	for _, v := range got.Variants {
		if !strings.HasPrefix(v.StorageRef, "products/cover/") {
			t.Errorf("rendition %q is not beside the original", v.StorageRef)
		}
		if !strings.Contains(v.StorageRef, "a1b2-photo") {
			t.Errorf("rendition %q dropped the original's unique base name", v.StorageRef)
		}
		if _, ok := store.objects[v.StorageRef]; !ok {
			t.Errorf("rendition %q was never written to storage (wrote %v)", v.StorageRef, store.keys())
		}
	}
}

func TestDeriveImagePlaceholderOptIn(t *testing.T) {
	cfg := landscapeConfig()
	cfg.Placeholder = &fwimage.PlaceholderOptions{Width: 20}
	d, err := imagefield.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got, err := d.DeriveImage(context.Background(), newMemStore(), testPNG(t, 400, 300), "a/b-x.png")
	if err != nil {
		t.Fatalf("DeriveImage: %v", err)
	}
	if !strings.HasPrefix(got.Placeholder, "data:image/") {
		t.Errorf("Placeholder = %.40q, want an inline raster data URL", got.Placeholder)
	}
}

// The derived metadata is persisted and later read back into a render path,
// so it has to satisfy the same invariants as the primary file.
func TestDerivedMetadataPassesFileFieldValidation(t *testing.T) {
	cfg := landscapeConfig()
	cfg.Placeholder = &fwimage.PlaceholderOptions{Width: 20}
	d, _ := imagefield.New(cfg)
	got, err := d.DeriveImage(context.Background(), newMemStore(), testPNG(t, 400, 300), "a/b-x.png")
	if err != nil {
		t.Fatalf("DeriveImage: %v", err)
	}
	if err := got.Validate(); err != nil {
		t.Errorf("derived metadata fails validation: %v", err)
	}
	ff := &file.FileField{URL: "/a/b-x.png", MimeType: "image/png", StorageRef: "a/b-x.png", Image: got}
	if err := ff.Validate(); err != nil {
		t.Errorf("FileField carrying derived metadata fails validation: %v", err)
	}
}

// A non-image on an image field must fail the upload, not store a file with
// no renditions that surfaces as a broken page much later.
func TestDeriveImageRejectsNonImage(t *testing.T) {
	d, _ := imagefield.New(landscapeConfig())
	_, err := d.DeriveImage(context.Background(), newMemStore(), []byte("this is not an image"), "a/b-x.bin")
	if err == nil {
		t.Fatal("expected an error for a non-image upload")
	}
}

func TestDeriveImagePropagatesStorageFailure(t *testing.T) {
	d, _ := imagefield.New(landscapeConfig())
	store := newMemStore()
	store.failOn = "-sm"
	_, err := d.DeriveImage(context.Background(), store, testPNG(t, 800, 600), "a/b-x.png")
	if err == nil {
		t.Fatal("expected a storage failure to surface")
	}
}

func TestDeriveImageClampsToSourceWidth(t *testing.T) {
	d, err := imagefield.New(imagefield.Config{
		Variants: []fwimage.Variant{{Width: 2048, Format: fwimage.FormatJPEG, Quality: 80, Suffix: "lg"}},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// A 64px source must not fan out into a 2048px pixel-multiplied file.
	got, err := d.DeriveImage(context.Background(), newMemStore(), testPNG(t, 64, 48), "a/b-x.png")
	if err != nil {
		t.Fatalf("DeriveImage: %v", err)
	}
	if len(got.Variants) != 1 {
		t.Fatalf("variants = %d, want 1", len(got.Variants))
	}
	if got.Variants[0].Width > 64 {
		t.Errorf("rendition upscaled to %dpx from a 64px source", got.Variants[0].Width)
	}
}

func TestNewRejectsBadConfig(t *testing.T) {
	cases := []struct {
		name string
		cfg  imagefield.Config
	}{
		{"derives nothing", imagefield.Config{}},
		{"half a blurhash", imagefield.Config{BlurHashX: 4}},
		{"blurhash out of range", imagefield.Config{BlurHashX: 10, BlurHashY: 10}},
		{"variant without width", imagefield.Config{Variants: []fwimage.Variant{{Format: fwimage.FormatJPEG}}}},
		{"variant without format", imagefield.Config{Variants: []fwimage.Variant{{Width: 100}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := imagefield.New(tc.cfg); err == nil {
				t.Errorf("expected an error for %+v", tc.cfg)
			}
		})
	}
}

func TestDeriveImageRequiresStore(t *testing.T) {
	d, _ := imagefield.New(landscapeConfig())
	if _, err := d.DeriveImage(context.Background(), nil, testPNG(t, 64, 48), "a/b.png"); err == nil {
		t.Fatal("expected an error without a storage backend")
	}
}

// The variants slice is persisted as JSON into a sibling column, so its
// shape is part of the contract a caller reads back.
func TestDerivedVariantsJSONShape(t *testing.T) {
	d, _ := imagefield.New(landscapeConfig())
	got, err := d.DeriveImage(context.Background(), newMemStore(), testPNG(t, 800, 600), "a/b-x.png")
	if err != nil {
		t.Fatalf("DeriveImage: %v", err)
	}
	raw, err := json.Marshal(got.Variants)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back []file.DerivedVariant
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(back) != len(got.Variants) || back[0].StorageRef != got.Variants[0].StorageRef {
		t.Errorf("round trip lost data: %s", raw)
	}
	for _, key := range []string{"storage_ref", "mime", "width", "height"} {
		if !bytes.Contains(raw, []byte(`"`+key+`"`)) {
			t.Errorf("JSON missing %q key: %s", key, raw)
		}
	}
}
