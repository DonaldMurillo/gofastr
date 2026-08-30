package file_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core/upload"
	"github.com/DonaldMurillo/gofastr/framework/file"
)

// TestFileField_RejectsJavaScriptScheme verifies Validate refuses a
// FileField whose URL begins with javascript: / data: / vbscript:,
// downstream renderers that drop the URL into href / src would XSS.
func TestFileField_RejectsJavaScriptScheme(t *testing.T) {
	t.Parallel()
	for _, url := range []string{
		"javascript:alert(1)",
		"JAVASCRIPT:alert(1)",
		"  javascript:alert(1)",
		"vbscript:msgbox(1)",
		"data:text/html,<script>alert(1)</script>",
		"data:application/xhtml+xml,<x/>",
		// Embedded TAB/LF/CR inside the scheme, browsers strip these
		// anywhere before resolving the scheme, so they must be rejected.
		"java\tscript:alert(1)",
		"java\nscript:alert(1)",
		"jav\rascript:alert(1)",
		"\x00javascript:alert(1)",
		// Leading C0 control bytes outside TAB/LF/CR/NUL, the WHATWG URL
		// spec strips ALL leading C0-control-or-space bytes (0x00-0x20)
		// before resolving the scheme, so the browser still executes
		// javascript: here. The guard must too.
		"\x0cjavascript:alert(1)",
		"\x0bjavascript:alert(1)",
		"\x01javascript:alert(1)",
		"\x1fdata:text/html,<script>alert(1)</script>",
	} {
		ff := &file.FileField{URL: url}
		err := ff.Validate()
		if err == nil {
			t.Errorf("SECURITY: [filefield] URL %q passed Validate", url)
			continue
		}
		if !errors.Is(err, file.ErrFileFieldURLScheme) {
			t.Errorf("URL %q: err = %v; want ErrFileFieldURLScheme", url, err)
		}
	}
}

// TestFileField_RejectsTraversal verifies path-traversal markers in URL
// or StorageRef are rejected, a downstream join could interpret them.
func TestFileField_RejectsTraversal(t *testing.T) {
	t.Parallel()
	cases := []*file.FileField{
		{URL: "../../../etc/passwd"},
		{URL: "static/../../private.pem"},
		{StorageRef: "../../../etc/passwd"},
		{StorageRef: "uploads/../private/key.pem"},
	}
	for _, ff := range cases {
		err := ff.Validate()
		if !errors.Is(err, file.ErrFileFieldTraversal) {
			t.Errorf("SECURITY: [filefield] %#v: err = %v; want ErrFileFieldTraversal", ff, err)
		}
	}
}

// TestFileField_RejectsXSSInMime verifies a MIME field containing
// angle brackets / script-tag-shaped tokens is rejected as malformed.
func TestFileField_RejectsXSSInMime(t *testing.T) {
	t.Parallel()
	for _, mt := range []string{
		"<script>alert(1)</script>",
		"text/html<script>",
		"text/html\";onerror=",
	} {
		ff := &file.FileField{MimeType: mt}
		err := ff.Validate()
		if !errors.Is(err, file.ErrFileFieldMimeUnsafe) {
			t.Errorf("SECURITY: [filefield] MIME %q: err = %v; want ErrFileFieldMimeUnsafe", mt, err)
		}
	}
}

// TestFileField_RejectsNegativeSize verifies a negative Size is rejected
// before reaching storage / database layers.
func TestFileField_RejectsNegativeSize(t *testing.T) {
	t.Parallel()
	for _, sz := range []int64{-1, -1024, -1 << 30} {
		ff := &file.FileField{Size: sz}
		err := ff.Validate()
		if !errors.Is(err, file.ErrFileFieldSize) {
			t.Errorf("SECURITY: [filefield] size %d: err = %v; want ErrFileFieldSize", sz, err)
		}
	}
}

// TestFileField_RejectsOversize verifies the per-string length cap fires
// for any of the four string fields, protects logs / DB columns from
// an attacker shipping a 100 KB MIME string.
func TestFileField_RejectsOversize(t *testing.T) {
	t.Parallel()
	big := strings.Repeat("a", file.MaxFileFieldStringBytes+1)
	for _, ff := range []*file.FileField{
		{URL: big},
		{Filename: big},
		{MimeType: big},
		{StorageRef: big},
	} {
		err := ff.Validate()
		if !errors.Is(err, file.ErrFileFieldOversize) {
			t.Errorf("SECURITY: [filefield] oversize: err = %v; want ErrFileFieldOversize", err)
		}
	}
}

// TestGenerateFilePath_NoCollision verifies repeated path generation for
// the same entity/field/filename never collides, uniqueness must not
// depend on clock resolution, or one upload silently overwrites another.
func TestGenerateFilePath_NoCollision(t *testing.T) {
	t.Parallel()
	const n = 1000
	seen := make(map[string]struct{}, n)
	for i := range n {
		p := file.GenerateFilePath("users", "avatar", "photo.png")
		if !strings.HasPrefix(p, "uploads/users/avatar/photo_") || !strings.HasSuffix(p, ".png") {
			t.Fatalf("unexpected path shape: %q", p)
		}
		if _, dup := seen[p]; dup {
			t.Fatalf("SECURITY: [filefield] GenerateFilePath collision after %d calls: %q", i, p)
		}
		seen[p] = struct{}{}
	}
}

// TestFileField_AcceptsLegitimate verifies typical FileFields pass.
func TestFileField_AcceptsLegitimate(t *testing.T) {
	t.Parallel()
	for _, ff := range []*file.FileField{
		{URL: "uploads/posts/avatar/photo_123.png", MimeType: "image/png", Size: 1024, StorageRef: "uploads/posts/avatar/photo_123.png"},
		{URL: "https://cdn.example.com/a.jpg", MimeType: "image/jpeg", Size: 0},
		{URL: "/static/file.pdf", MimeType: "application/pdf", Size: 500},
		nil,
	} {
		if err := ff.Validate(); err != nil {
			t.Errorf("legitimate field rejected: %v", err)
		}
	}
}

// TestPlaceholderAllowListsAgree is the cross-check the comment on
// file.isRasterDataURL promises: the LQIP allow-list enforced when a
// FileField is validated must accept exactly what the render sink will
// actually paint. The sink is framework/ui.placeholderUsable, which is
// core-ui/urlsafe's ImageSource policy restricted to the data: scheme.
// Asserting against urlsafe directly (rather than framework/ui) keeps
// framework/file a leaf, this is a test-only edge.
//
// The two must agree in BOTH directions. A value this package accepts
// but the sink drops is a silently broken placeholder; a value this
// package accepts that the sink would paint from the network is a
// tracking beacon persisted through an image column.
func TestPlaceholderAllowListsAgree(t *testing.T) {
	t.Parallel()
	corpus := []string{
		"data:image/jpeg;base64,AAAA",
		"data:image/png;base64,AAAA",
		"data:image/gif;base64,AAAA",
		"data:image/webp;base64,AAAA",
		"data:image/avif;base64,AAAA",
		"data:image/svg+xml;base64,AAAA",
		"data:text/html,<script>alert(1)</script>",
		"javascript:alert(1)",
		"https://evil.example/px.gif",
		"HTTPS://evil.example/px.gif",
		"//evil.example/px.gif",
		"/uploads/px.gif",
		"px.gif",
	}
	for _, c := range corpus {
		d := &file.ImageDerivatives{Placeholder: c}
		accepted := d.Validate() == nil
		// The render sink: data: scheme AND urlsafe.ImageSource.
		painted := strings.HasPrefix(strings.ToLower(c), "data:") &&
			urlsafe.OK(c, urlsafe.ImageSource)
		if accepted != painted {
			t.Fatalf("SECURITY: [filefield] placeholder %q: Validate accepted=%v but render sink paints=%v. The two allow-lists must agree; a remote URL stored as a placeholder is a beacon, and a rejected-at-render value is a silently dead LQIP.", c, accepted, painted)
		}
	}
}

// TestFileFieldRejectsControlBytes pins one property, no C0 control
// byte or DEL survives validation, across every FileField string
// surface that is persisted and later echoed into a header, an HTML
// attribute, or a log line. mimeType is already covered by its
// charset filter; url, filename, and storageRef were not.
func TestFileFieldRejectsControlBytes(t *testing.T) {
	t.Parallel()
	payloads := map[string]string{
		"cr":  "ok\rinjected",
		"lf":  "ok\ninjected",
		"nul": "ok\x00.jpg",
		"del": "ok\x7finjected",
	}
	surfaces := map[string]func(string) *file.FileField{
		"url":        func(v string) *file.FileField { return &file.FileField{URL: v} },
		"filename":   func(v string) *file.FileField { return &file.FileField{Filename: v} },
		"mimeType":   func(v string) *file.FileField { return &file.FileField{MimeType: v} },
		"storageRef": func(v string) *file.FileField { return &file.FileField{StorageRef: v} },
	}
	for sname, mk := range surfaces {
		for pname, payload := range payloads {
			if mk(payload).Validate() == nil {
				t.Errorf("SECURITY: [filefield] Validate accepted %s control byte in %s. Attack: the stored value is echoed into Content-Disposition / a log line / an HTML attribute and splits it.", pname, sname)
			}
		}
	}
}

// TestFileFieldRejectsFilenameTraversal pins that Filename is held to
// the same traversal rule as url and storage_ref. A `..` filename
// reaches Content-Disposition and any host that joins it onto a path.
func TestFileFieldRejectsFilenameTraversal(t *testing.T) {
	t.Parallel()
	for _, n := range []string{"../../etc/passwd", "a/../../b.png", ".."} {
		ff := &file.FileField{Filename: n}
		if err := ff.Validate(); !errors.Is(err, file.ErrFileFieldTraversal) {
			t.Errorf("SECURITY: [filefield] Validate accepted traversal filename %q (err=%v)", n, err)
		}
	}
}

// TestProcessFileFieldStripsCtrlInName pins the doc claim at
// FileField.Validate that "the constructors in this package
// (ProcessFileField) already produce valid FileFields", a hostile
// multipart filename must not be able to produce a FileField that
// Validate would reject.
func TestProcessFileFieldStripsCtrlInName(t *testing.T) {
	t.Parallel()
	png := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0, 0, 0, 0x0D, 'I', 'H', 'D', 'R'}
	ff, err := file.ProcessFileField(context.Background(), &captureStorage{},
		bytes.NewReader(png), "a\r\nContent-Disposition: x\x00.png", "posts", "cover")
	if err != nil {
		t.Fatalf("legitimate PNG upload rejected: %v", err)
	}
	if verr := ff.Validate(); verr != nil {
		t.Fatalf("SECURITY: [filefield] ProcessFileField produced a FileField its own Validate rejects (%v) for filename %q. Attack: CRLF in the echoed filename splits Content-Disposition.", verr, ff.Filename)
	}
}

// TestProcessFileFieldMeetsOwnContract pins the documented constructor
// invariant ("The constructors in this package (ProcessFileField)
// already produce valid FileFields", filefield.go): whatever the client
// filename, ProcessFileField must either fail or return a FileField
// that passes its own Validate. The constructor sets Filename via
// filepath.Base(stripControlBytes(...)), which removes control bytes
// but keeps interior dot-pairs, while Validate's hasTraversal is a
// substring match, so a plain "v2..final.png" upload yields stored
// metadata that Validate rejects. The storage key is unaffected
// (UniqueFilename collapses ".." to "_"); the exposed value is the
// persisted Filename, which reaches Content-Disposition and any host
// that joins it onto a path.
func TestProcessFileFieldMeetsOwnContract(t *testing.T) {
	t.Parallel()
	png := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0, 0, 0, 0x0D, 'I', 'H', 'D', 'R'}
	for _, name := range []string{"v2..final.png", "a...b.txt", "report..2.pdf"} {
		ff, err := file.ProcessFileField(context.Background(), &captureStorage{}, bytes.NewReader(png), name, "posts", "cover")
		if err != nil {
			t.Fatalf("legitimate PNG upload rejected for filename %q: %v", name, err)
		}
		if verr := ff.Validate(); verr != nil {
			t.Errorf("SECURITY: [filefield] ProcessFileField accepted client filename %q but produced a FileField its own Validate rejects (Filename=%q, err=%v). Attack: the traversal-bearing Filename is persisted and reaches Content-Disposition and host path joins.", name, ff.Filename, verr)
		}
	}

	// Same invariant through the only other FileField-producing surface
	// in this package: the constructor with a deriver attached.
	ff, err := file.ProcessFileField(context.Background(), &captureStorage{},
		bytes.NewReader(png), "v2..final.png", "posts", "cover",
		file.WithImageDeriver(stubDeriver{}))
	if err != nil {
		t.Fatalf("legitimate PNG upload rejected: %v", err)
	}
	if verr := ff.Validate(); verr != nil {
		t.Errorf("SECURITY: [filefield] ProcessFileField+deriver produced a FileField its own Validate rejects (Filename=%q, err=%v)", ff.Filename, verr)
	}
}

// stubDeriver is the minimal ImageDeriver: one clean variant derived
// from the primary storage key.
type stubDeriver struct{}

func (stubDeriver) DeriveImage(ctx context.Context, store upload.Storage, data []byte, primaryRef string) (*file.ImageDerivatives, error) {
	return &file.ImageDerivatives{
		Variants: []file.DerivedVariant{{
			StorageRef: primaryRef + "_480.webp",
			MIME:       "image/webp",
			Width:      480,
			Height:     320,
		}},
	}, nil
}

// TestProcessFileFieldStripsUnicodeBreaks extends the control-strip
// family to the Unicode line terminators: U+2028/U+2029/U+0085 are all
// bytes >=0x80, so framework/file's byte-wise stripControlBytes keeps
// them, while core/upload's rune-aware SanitizeFilename (which builds
// the storage key) strips them. The stored Filename must not carry a
// codepoint that JS/JSON/log tooling treats as a line break. Asserted
// at the constructor surface: Validate's own doc scopes it to C0/DEL
// and its scan is byte-wise, so it is not the gate for these.
func TestProcessFileFieldStripsUnicodeBreaks(t *testing.T) {
	t.Parallel()
	png := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0, 0, 0, 0x0D, 'I', 'H', 'D', 'R'}
	cases := map[string]rune{
		"line-separator": '\u2028',
		"paragraph-sep":  '\u2029',
		"nel":            '\u0085',
	}
	for cname, sep := range cases {
		ff, err := file.ProcessFileField(context.Background(), &captureStorage{},
			bytes.NewReader(png), "pic"+string(sep)+".png", "posts", "cover")
		if err != nil {
			t.Fatalf("legitimate PNG upload rejected for %s filename: %v", cname, err)
		}
		if strings.ContainsRune(ff.Filename, sep) {
			t.Errorf("SECURITY: [filefield] ProcessFileField kept %s (U+%04X) in stored Filename %q. Attack: a Unicode line terminator in the persisted filename injects a line break into JS/JSON/log consumers, the same hazard CR/LF stripping exists to close.", cname, sep, ff.Filename)
		}
	}
}

// TestVariantRefRejectsControlBytes extends the no-C0/DEL property
// (TestFileFieldRejectsControlBytes, primary surfaces) to the derived
// variant surface. ImageDerivatives.Validate's doc promises "the same
// invariants" as the primary file and variant refs reach the same
// sinks (an <img src>, a storage delete), but the variant loop checks
// oversize/scheme/traversal/MIME/dimensions and never runs the
// control-byte scan the primary loop applies.
func TestVariantRefRejectsControlBytes(t *testing.T) {
	t.Parallel()
	payloads := map[string]string{
		"cr":  "uploads/posts/cover/a\rb.webp",
		"lf":  "uploads/posts/cover/a\nb.webp",
		"nul": "uploads/posts/cover/a\x00b.webp",
		"del": "uploads/posts/cover/a\x7fb.webp",
	}
	for pname, ref := range payloads {
		dv := &file.ImageDerivatives{Variants: []file.DerivedVariant{{
			StorageRef: ref, MIME: "image/webp", Width: 480, Height: 320,
		}}}
		if err := dv.Validate(); !errors.Is(err, file.ErrFileFieldControlBytes) {
			t.Errorf("SECURITY: [filefield] ImageDerivatives.Validate accepted %s control byte in variant storage_ref %q (err=%v). Attack: the ref is persisted in the <field>_variants column and reaches the same <img src>/storage-delete sinks as the primary storage_ref.", pname, ref, err)
		}
		// Same answer when the variants ride in on a FileField, the
		// shape a host round-trips from the persisted column.
		ff := &file.FileField{StorageRef: "uploads/posts/cover/a.webp", MimeType: "image/webp", Size: 1, Image: dv}
		if err := ff.Validate(); !errors.Is(err, file.ErrFileFieldControlBytes) {
			t.Errorf("SECURITY: [filefield] FileField.Validate accepted variant storage_ref carrying %s control byte %q (err=%v)", pname, ref, err)
		}
	}
}

// TestDeleteFileField_RemovesVariants pins CHAIN8-R3 (deletion lifecycle,
// contract conflict, MEDIUM). framework/file/imagederive.go:48-51 justifies
// holding derived references to the primary file's invariants because:
//
//	// Validate enforces the same invariants on derived references that
//	// FileField.Validate enforces on the primary file, they reach the same
//	// sinks (an <img src>, a storage delete) and arrive from the same
//	// untrusted places once persisted and read back.
//
// and every rendition is a distinct storage object written through store.Save
// (framework/imagefield/imagefield.go:160-168). But DeleteFileField
// (filefield.go:560-567), the framework's only storage-delete primitive —
// the "a storage delete" sink that contract names — reads ff.StorageRef only
// and never consults ff.Image:
//
//	func DeleteFileField(ctx context.Context, store upload.Storage, ff *FileField) error {
//		if store == nil {
//			return fmt.Errorf("filefield: storage backend is required")
//		}
//		if ff == nil || ff.StorageRef == "" {
//			return nil
//		}
//		return store.Delete(ctx, ff.StorageRef)
//	}
//
// A host that dutifully calls DeleteFileField on entity delete/update (the
// wiring CHAIN8-R2 asks for) still leaves every _sm/_md rendition stored and
// downloadable at the documented /uploads/{key...} route. Observable through
// the real backend: after DeleteFileField succeeds, the rendition object
// recorded in ff.Image.Variants must no longer be readable from storage.
// The conflicting sides are quoted above for triage; only the variant-removal
// behavior is asserted.
func TestDeleteFileField_RemovesVariants(t *testing.T) {
	t.Parallel()
	store := upload.NewLocalStorage(t.TempDir())
	ctx := context.Background()

	primary := "uploads/profiles/avatar/avatar_1_ab.png"
	variant := "uploads/profiles/avatar/avatar_1_ab_sm.webp"
	if err := store.Save(ctx, primary, strings.NewReader("primary-bytes")); err != nil {
		t.Fatalf("save primary: %v", err)
	}
	if err := store.Save(ctx, variant, strings.NewReader("variant-bytes")); err != nil {
		t.Fatalf("save variant: %v", err)
	}

	ff := &file.FileField{
		URL:        primary,
		StorageRef: primary,
		Image: &file.ImageDerivatives{
			Variants: []file.DerivedVariant{
				{StorageRef: variant, MIME: "image/webp", Width: 320, Height: 240},
			},
		},
	}
	if err := file.DeleteFileField(ctx, store, ff); err != nil {
		t.Fatalf("DeleteFileField: %v", err)
	}

	// The primary is gone: proves the delete reached the backend (this leg
	// is green today and must stay green after any fix).
	if _, err := store.Get(ctx, primary); err == nil {
		t.Errorf("primary object %q survived DeleteFileField", primary)
	}

	// The rendition must be gone too. Today Get returns a reader with the
	// stored bytes: the orphan survives even correct cleanup wiring.
	if _, err := store.Get(ctx, variant); err == nil {
		t.Errorf("SECURITY: [CHAIN8-R3] DeleteFileField removed the primary object but the stored rendition %q is still readable from storage. imagederive.go:48-51 promises derived references reach \"a storage delete\" sink; filefield.go:560-567 never reads ff.Image.Variants, so every rendition outlives the row's cleanup and keeps serving at the documented /uploads/{key...} route.", variant)
	}
	if ok, err := store.Exists(ctx, variant); err == nil && ok {
		t.Errorf("SECURITY: [CHAIN8-R3] rendition %q still exists in storage after DeleteFileField", variant)
	}
}
