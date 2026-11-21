package file

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/upload"
)

type memStore struct {
	saved   map[string][]byte
	deletes []string
	failDel map[string]bool
}

func newMemStore() *memStore {
	return &memStore{saved: map[string][]byte{}, failDel: map[string]bool{}}
}

func (s *memStore) Save(_ context.Context, key string, r io.Reader) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	s.saved[key] = b
	return nil
}

func (s *memStore) Delete(_ context.Context, key string) error {
	s.deletes = append(s.deletes, key)
	if s.failDel[key] {
		return errors.New("backend down")
	}
	if _, ok := s.saved[key]; !ok {
		return os.ErrNotExist
	}
	delete(s.saved, key)
	return nil
}

func (s *memStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	b, ok := s.saved[key]
	if !ok {
		return nil, os.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (s *memStore) Exists(_ context.Context, key string) (bool, error) {
	_, ok := s.saved[key]
	return ok, nil
}

func TestRejectUnsafeContentTable(t *testing.T) {
	png := "\x89PNG\r\n\x1a\n\x00\x00\x00\x0dIHDR"
	pad := strings.Repeat("a", 600)
	reject := map[string]string{
		"elf":                 "\x7fELF\x02\x01\x01",
		"macho-feedfacf":      "\xfe\xed\xfa\xcf....",
		"macho-cffaedfe":      "\xcf\xfa\xed\xfe....",
		"macho-cafebabe":      "\xca\xfe\xba\xbe....",
		"macho-bebafeca":      "\xbe\xba\xfe\xca....",
		"macho-cefaedfe":      "\xce\xfa\xed\xfe....",
		"div-sniffed":         "<div>hi</div>",
		"soft-late-in-window": strings.Repeat("a", 200) + "<link rel=x>",
		"soft-js-upper":       "go to JAVASCRIPT:alert(1)",
		"soft-js":             "go to javascript:alert(1)",
		"soft-case":           "x <ImG src=x>",
		"hard-upper":          "x <SCRIPT>",
		"hard-mixed":          "x <sCrIpT>",
	}
	for _, tok := range []string{"<math", "<base", "<svg", "<script", "<iframe", "<html", "<!doctype", "<object", "<embed"} {
		reject["hard-past-window-"+tok] = png + pad + tok
	}
	for name, body := range reject {
		if err := rejectUnsafeContent([]byte(body)); !errors.Is(err, ErrFileFieldUnsafeContent) {
			t.Errorf("%s: err = %v, want ErrFileFieldUnsafeContent", name, err)
		}
	}
	accept := map[string]string{
		"soft-past-window": pad + "<img src=x>",
		"zip-soft-in-head": "PK\x03\x04" + "<img src=x>",
		"png-soft-in-head": png + "<img src=x>",
	}
	for name, body := range accept {
		if err := rejectUnsafeContent([]byte(body)); err != nil {
			t.Errorf("%s: err = %v, want accepted", name, err)
		}
	}
}

func TestProcessFileFieldTypeFromNameOnlyForUnknownBytes(t *testing.T) {
	store := newMemStore()
	ff, err := ProcessFileField(context.Background(), store, bytes.NewReader([]byte{0, 1, 2, 3, 4, 5}), "report.pdf", "e", "f")
	if err != nil {
		t.Fatal(err)
	}
	if ff.MimeType != "application/pdf" {
		t.Fatalf("MimeType = %q, want application/pdf from the name", ff.MimeType)
	}
}

func TestProcessAndDeleteNeedStorage(t *testing.T) {
	if _, err := ProcessFileField(context.Background(), nil, strings.NewReader("x"), "a.txt", "e", "f"); err == nil {
		t.Error("ProcessFileField accepted a nil store")
	}
	if err := DeleteFileField(context.Background(), nil, &FileField{StorageRef: "k"}); err == nil {
		t.Error("DeleteFileField accepted a nil store")
	}
	if err := DeleteFileField(context.Background(), newMemStore(), nil); err != nil {
		t.Errorf("DeleteFileField(nil field) = %v", err)
	}
}

type invalidDeriver struct{}

func (invalidDeriver) DeriveImage(ctx context.Context, store upload.Storage, _ []byte, primary string) (*ImageDerivatives, error) {
	if err := store.Save(ctx, primary+".sm", strings.NewReader("r")); err != nil {
		return nil, err
	}
	return &ImageDerivatives{Variants: []DerivedVariant{{StorageRef: primary + "/../x"}}}, nil
}

func TestInvalidDerivativesRollBack(t *testing.T) {
	store := newMemStore()
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\x0dIHDR")
	_, err := ProcessFileField(context.Background(), store, bytes.NewReader(png), "a.png", "e", "f", WithImageDeriver(invalidDeriver{}))
	if !errors.Is(err, ErrFileFieldTraversal) {
		t.Fatalf("err = %v, want ErrFileFieldTraversal", err)
	}
	if len(store.saved) != 0 {
		t.Fatalf("objects left after rejected derivatives: %v", store.saved)
	}
}

func TestDeleteFileFieldContract(t *testing.T) {
	ctx := context.Background()
	store := newMemStore()
	store.saved["p"] = []byte("x")
	ff := &FileField{StorageRef: "p", Image: &ImageDerivatives{Variants: []DerivedVariant{{StorageRef: "p"}, {StorageRef: "gone"}, {}}}}
	if err := DeleteFileField(ctx, store, ff); err != nil {
		t.Fatalf("a missing rendition is not an error: %v", err)
	}
	if got := strings.Join(store.deletes, ","); got != "gone,p" {
		t.Fatalf("deletes = %q, want gone,p (variant equal to primary and empty refs skipped)", got)
	}
	store = newMemStore()
	store.saved["p"], store.saved["v"] = []byte("x"), []byte("y")
	store.failDel["v"] = true
	ff = &FileField{StorageRef: "p", Image: &ImageDerivatives{Variants: []DerivedVariant{{StorageRef: "v"}}}}
	if err := DeleteFileField(ctx, store, ff); err == nil || !strings.Contains(err.Error(), "v (") {
		t.Fatalf("one failed delete must be reported, got %v", err)
	}
	if _, ok := store.saved["p"]; ok {
		t.Fatal("the primary was not attempted after a rendition failed")
	}
}

func TestImageDerivativesValidateTable(t *testing.T) {
	long := strings.Repeat("a", MaxFileFieldStringBytes+1)
	cases := map[string]struct {
		d    ImageDerivatives
		want error
	}{
		"ref-oversize":        {ImageDerivatives{Variants: []DerivedVariant{{StorageRef: long}}}, ErrFileFieldOversize},
		"ref-scheme":          {ImageDerivatives{Variants: []DerivedVariant{{StorageRef: "javascript:x"}}}, ErrFileFieldURLScheme},
		"ref-traversal":       {ImageDerivatives{Variants: []DerivedVariant{{StorageRef: "a/../b"}}}, ErrFileFieldTraversal},
		"ref-bidi":            {ImageDerivatives{Variants: []DerivedVariant{{StorageRef: "a‮b"}}}, ErrFileFieldControlBytes},
		"mime-oversize":       {ImageDerivatives{Variants: []DerivedVariant{{MIME: "image/" + long}}}, ErrFileFieldOversize},
		"mime-bidi":           {ImageDerivatives{Variants: []DerivedVariant{{MIME: "image/​webp"}}}, ErrFileFieldControlBytes},
		"mime-control":        {ImageDerivatives{Variants: []DerivedVariant{{MIME: "image/webp\r\n"}}}, ErrFileFieldControlBytes},
		"mime-unsafe":         {ImageDerivatives{Variants: []DerivedVariant{{MIME: "image/<x>"}}}, ErrFileFieldMimeUnsafe},
		"height-negative":     {ImageDerivatives{Variants: []DerivedVariant{{Height: -1}}}, ErrFileFieldSize},
		"placeholder-long":    {ImageDerivatives{Placeholder: "data:image/png," + long}, ErrFileFieldOversize},
		"blurhash-long":       {ImageDerivatives{BlurHash: long}, ErrFileFieldOversize},
		"placeholder-remote":  {ImageDerivatives{Placeholder: "https://t.example/p.gif"}, ErrFileFieldURLScheme},
		"placeholder-no-type": {ImageDerivatives{Placeholder: "data:image/,x"}, ErrFileFieldURLScheme},
	}
	for name, c := range cases {
		if err := c.d.Validate(); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", name, err, c.want)
		}
	}
	ok := ImageDerivatives{Placeholder: "DATA:IMAGE/PNG;base64,AAAA"}
	if err := ok.Validate(); err != nil {
		t.Errorf("an upper-case raster data: URL was refused: %v", err)
	}
}

func TestVariantStorageRefsAndExternalURL(t *testing.T) {
	refs, ok := VariantStorageRefs([]byte(`[{"storage_ref":"a"},{"storage_ref":""},{"mime":"x"}]`))
	if !ok || len(refs) != 1 || refs[0] != "a" {
		t.Errorf("refs = %v %v, want [a] true", refs, ok)
	}
	if _, ok := VariantStorageRefs([]byte(`{"storage_ref":"a"}`)); ok {
		t.Error("a JSON object was read as a variants array")
	}
	if refs, ok := VariantStorageRefs([]byte(`null`)); ok || len(refs) != 0 {
		t.Errorf("null: refs = %v, ok = %v, want no refs and false", refs, ok)
	}
	if refs, ok := VariantStorageRefs([]byte(`[]`)); !ok || len(refs) != 0 {
		t.Errorf("[]: refs = %v, ok = %v, want no refs and true", refs, ok)
	}
	for s, want := range map[string]bool{"http://h/x": true, "HTTPS://h/x": true, "/uploads/x": false, "ftp://h/x": false, "http:///x": false} {
		if got := IsExternalURL(s); got != want {
			t.Errorf("IsExternalURL(%q) = %v, want %v", s, got, want)
		}
	}
}
