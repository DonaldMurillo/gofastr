package image

import (
	"bytes"
	"errors"
	stdimage "image"
	"image/color"
	"image/gif"
	"io"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestVariantSetEmptyOK(t *testing.T) {
	src := FromImage(gradient(40, 30), FormatPNG)
	res, err := VariantSet{}.Process(src)
	if err != nil {
		t.Fatalf("empty set: %v", err)
	}
	if len(res.Variants) != 0 {
		t.Errorf("expected no variants, got %d", len(res.Variants))
	}
	if res.Placeholder != "" || res.BlurHash != "" {
		t.Errorf("expected no placeholder/hash, got %q / %q", res.Placeholder, res.BlurHash)
	}
	if res.SourceWidth != 40 || res.SourceHeight != 30 {
		t.Errorf("source dims = %d×%d, want 40×30", res.SourceWidth, res.SourceHeight)
	}
}

func TestVariantSetProducesEachVariant(t *testing.T) {
	src := FromImage(gradient(400, 300), FormatPNG)
	set := VariantSet{
		BaseName: "photo",
		Variants: []Variant{
			{Width: 100, Format: FormatJPEG, Quality: 80, Suffix: "sm"},
			{Width: 200, Format: FormatPNG},
			{Width: 50, Format: FormatGIF},
		},
	}
	res, err := set.Process(src)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if len(res.Variants) != 3 {
		t.Fatalf("expected 3 variants, got %d", len(res.Variants))
	}

	want := []struct {
		name   string
		mime   string
		width  int
		format Format
	}{
		{"photo-sm.jpg", "image/jpeg", 100, FormatJPEG},
		{"photo-200.png", "image/png", 200, FormatPNG},
		{"photo-50.gif", "image/gif", 50, FormatGIF},
	}
	for i, v := range res.Variants {
		if v.Name != want[i].name {
			t.Errorf("variants[%d].Name = %q, want %q", i, v.Name, want[i].name)
		}
		if v.MIME != want[i].mime {
			t.Errorf("variants[%d].MIME = %q, want %q", i, v.MIME, want[i].mime)
		}
		if v.Width != want[i].width {
			t.Errorf("variants[%d].Width = %d, want %d", i, v.Width, want[i].width)
		}
		if v.Format != want[i].format {
			t.Errorf("variants[%d].Format = %v, want %v", i, v.Format, want[i].format)
		}
		if len(v.Bytes) == 0 {
			t.Errorf("variants[%d].Bytes empty", i)
		}
		// Confirm decoded bytes match advertised format.
		decoded, err := DecodeBytes(v.Bytes)
		if err != nil {
			t.Errorf("variants[%d] re-decode: %v", i, err)
			continue
		}
		if decoded.Format() != v.Format {
			t.Errorf("variants[%d] re-decode format = %v, want %v", i, decoded.Format(), v.Format)
		}
		if decoded.Bounds().Dx() != v.Width {
			t.Errorf("variants[%d] decoded width = %d, want %d", i, decoded.Bounds().Dx(), v.Width)
		}
	}
}

func TestVariantSetPlaceholderAndBlurHash(t *testing.T) {
	src := FromImage(gradient(200, 150), FormatPNG)
	set := VariantSet{
		Placeholder: &PlaceholderOptions{Width: 16},
		BlurHashX:   4,
		BlurHashY:   3,
	}
	res, err := set.Process(src)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if !strings.HasPrefix(res.Placeholder, "data:image/jpeg;base64,") {
		t.Errorf("placeholder prefix wrong: %q", res.Placeholder[:30])
	}
	if len(res.BlurHash) != 28 {
		t.Errorf("BlurHash length = %d, want 28 (4×3)", len(res.BlurHash))
	}
	if res.BlurHash[0] != 'L' {
		t.Errorf("BlurHash size flag = %q, want 'L'", res.BlurHash[0])
	}
}

func TestVariantSetRejectsHalfBlurHash(t *testing.T) {
	src := FromImage(solidRGBA(8, 8, color.RGBA{1, 2, 3, 4}), FormatPNG)
	_, err := VariantSet{BlurHashX: 4}.Process(src)
	if err == nil {
		t.Fatal("expected error for half-configured BlurHash")
	}
}

func TestVariantSetRejectsZeroWidth(t *testing.T) {
	src := FromImage(solidRGBA(8, 8, color.RGBA{1, 2, 3, 4}), FormatPNG)
	_, err := VariantSet{Variants: []Variant{{Width: 0, Format: FormatJPEG}}}.Process(src)
	if err == nil {
		t.Fatal("expected error for Width=0")
	}
}

func TestVariantSetRejectsUnknownFormat(t *testing.T) {
	src := FromImage(solidRGBA(8, 8, color.RGBA{1, 2, 3, 4}), FormatPNG)
	_, err := VariantSet{Variants: []Variant{{Width: 64, Format: FormatUnknown}}}.Process(src)
	if err == nil {
		t.Fatal("expected error for FormatUnknown")
	}
}

func TestVariantSetEmitsWebPLossless(t *testing.T) {
	src := FromImage(gradient(64, 48), FormatPNG)
	res, err := VariantSet{Variants: []Variant{{Width: 32, Format: FormatWebP}}}.Process(src)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if len(res.Variants) != 1 || res.Variants[0].MIME != "image/webp" {
		t.Fatalf("expected one image/webp variant, got %+v", res.Variants)
	}
	// Round-trip: decoded width must match the requested size.
	decoded, err := DecodeBytes(res.Variants[0].Bytes)
	if err != nil {
		t.Fatalf("re-decode WebP: %v", err)
	}
	if decoded.Format() != FormatWebP {
		t.Errorf("re-decoded format = %v, want WebP", decoded.Format())
	}
	if decoded.Bounds().Dx() != 32 {
		t.Errorf("re-decoded width = %d, want 32", decoded.Bounds().Dx())
	}
}

// TestVariantSetProcessToStreams asserts that ProcessTo delivers
// each variant via a sink callback (one at a time) without
// materialising the full result set in memory, and that the
// returned StreamResult still carries Placeholder + BlurHash.
func TestVariantSetProcessToStreams(t *testing.T) {
	src := FromImage(gradient(200, 150), FormatPNG)
	set := VariantSet{
		BaseName: "photo",
		Variants: []Variant{
			{Width: 100, Format: FormatJPEG, Quality: 80, Suffix: "sm"},
			{Width: 200, Format: FormatPNG, Suffix: "md"},
		},
		Placeholder: &PlaceholderOptions{Width: 16},
		BlurHashX:   4,
		BlurHashY:   3,
	}
	gotNames := []string{}
	gotBytes := []int{}
	sr, err := set.ProcessTo(src, func(h VariantHeader, r io.Reader) error {
		data, rerr := io.ReadAll(r)
		if rerr != nil {
			return rerr
		}
		gotNames = append(gotNames, h.Name)
		gotBytes = append(gotBytes, len(data))
		return nil
	})
	if err != nil {
		t.Fatalf("ProcessTo: %v", err)
	}
	if len(gotNames) != 2 {
		t.Fatalf("expected 2 sink invocations, got %d (%v)", len(gotNames), gotNames)
	}
	if gotNames[0] != "photo-sm.jpg" || gotNames[1] != "photo-md.png" {
		t.Errorf("names: %v", gotNames)
	}
	if gotBytes[0] == 0 || gotBytes[1] == 0 {
		t.Errorf("empty variant bytes: %v", gotBytes)
	}
	if !strings.HasPrefix(sr.Placeholder, "data:image/jpeg;base64,") {
		t.Errorf("placeholder missing: %q", sr.Placeholder[:30])
	}
	if len(sr.BlurHash) != 28 {
		t.Errorf("BlurHash length = %d, want 28", len(sr.BlurHash))
	}
}

// TestVariantSetProcessToStopsOnSinkError surfaces sink errors and
// halts further variant emission.
func TestVariantSetProcessToStopsOnSinkError(t *testing.T) {
	src := FromImage(gradient(64, 48), FormatPNG)
	set := VariantSet{
		Variants: []Variant{
			{Width: 32, Format: FormatJPEG, Suffix: "a"},
			{Width: 16, Format: FormatJPEG, Suffix: "b"},
		},
	}
	calls := 0
	_, err := set.ProcessTo(src, func(h VariantHeader, r io.Reader) error {
		calls++
		return errors.New("sink boom")
	})
	if err == nil {
		t.Fatal("expected error from sink")
	}
	if calls != 1 {
		t.Errorf("sink should stop after first error; got %d calls", calls)
	}
}

// TestProcessToSinkReaderInvalidAfterReturn pins the contract:
// readers handed to the sink become invalid once the sink returns,
// so a sink that stashes one and reads it later gets an error rather
// than corrupted bytes from the next variant. Before the fix the
// reader was the *bytes.Buffer the framework reused across variants,
// so a late read would silently see the next variant's payload.
// TestProcessToDoesNotUpscaleByDefault mirrors the Process-side
// regression: ProcessTo must respect AllowUpscale too. Round 2
// added the clamp to Process but forgot ProcessTo, so callers
// reaching for the streaming path silently 16× their storage on
// any small source.
// TestProcessToReleasesIntermediatesBetweenVariants pins the streaming
// memory promise. Before the fix, scaled *Image references survived
// each loop iteration until ProcessTo returned, so the peak resident
// memory was sum-of-all-resized-buffers — the same as Process. After
// the fix, each iteration drops its reference so the GC can reclaim
// the previous variant's working buffer.
// TestVariantSetRejectAnimated pins the API for the avatar upload
// case: callers can opt-in to rejecting animated sources so the
// variant pipeline doesn't silently flatten N-1 frames.
func TestVariantSetRejectAnimated(t *testing.T) {
	// Build a 2-frame GIF and decode it through the framework.
	var gifBuf bytes.Buffer
	g := &gif.GIF{LoopCount: 0}
	for i := 0; i < 2; i++ {
		f := stdimage.NewPaletted(stdimage.Rect(0, 0, 8, 8),
			color.Palette{color.RGBA{0, 0, 0, 255}, color.RGBA{uint8(i * 200), 50, 50, 255}})
		g.Image = append(g.Image, f)
		g.Delay = append(g.Delay, 1)
	}
	if err := gif.EncodeAll(&gifBuf, g); err != nil {
		t.Fatalf("EncodeAll: %v", err)
	}
	img, err := DecodeBytes(gifBuf.Bytes())
	if err != nil {
		t.Fatalf("DecodeBytes: %v", err)
	}

	// Default behaviour: silent first-frame variant (no error).
	if _, err := (VariantSet{
		Variants: []Variant{{Width: 8, Format: FormatPNG, Suffix: "a"}},
	}).Process(img); err != nil {
		t.Errorf("default Process should not reject animated: %v", err)
	}

	// Opt-in: reject animated, surfacing ErrAnimatedSource.
	_, err = (VariantSet{
		RejectAnimated: true,
		Variants:       []Variant{{Width: 8, Format: FormatPNG, Suffix: "a"}},
	}).Process(img)
	if !errors.Is(err, ErrAnimatedSource) {
		t.Errorf("expected ErrAnimatedSource, got %v", err)
	}
	// ProcessTo path too.
	_, err = (VariantSet{
		RejectAnimated: true,
		Variants:       []Variant{{Width: 8, Format: FormatPNG, Suffix: "a"}},
	}).ProcessTo(img, func(VariantHeader, io.Reader) error { return nil })
	if !errors.Is(err, ErrAnimatedSource) {
		t.Errorf("ProcessTo expected ErrAnimatedSource, got %v", err)
	}
}

// TestBlurHashParityAcrossPaths pins that VariantSet.Process and
// direct img.BlurHash on the same source produce identical hashes.
// Previously VariantSet pre-resized to 32 px (round-1) and BlurHash
// auto-resized to 64 px (round-3) — two resize stages produced
// different output. Drop the pre-resize; BlurHash owns the dwn-scale.
// TestVariantSetRejectsExcessiveVariants pins the round-4 DoS defence:
// an attacker-controlled set of 10k variants must error fast (before
// any encoding) rather than chew CPU + RAM proportional to N.
func TestVariantSetRejectsExcessiveVariants(t *testing.T) {
	src := FromImage(gradient(64, 48), FormatPNG)
	excess := make([]Variant, 100)
	for i := range excess {
		excess[i] = Variant{Width: 16, Format: FormatPNG, Suffix: strconv.Itoa(i)}
	}
	_, err := (VariantSet{Variants: excess}).Process(src)
	if err == nil || !strings.Contains(err.Error(), "too many variants") {
		t.Errorf("Process: expected too-many-variants error; got %v", err)
	}
	_, err = (VariantSet{Variants: excess}).ProcessTo(src, func(VariantHeader, io.Reader) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "too many variants") {
		t.Errorf("ProcessTo: expected too-many-variants error; got %v", err)
	}
}

func TestBlurHashParityAcrossPaths(t *testing.T) {
	src := FromImage(gradient(256, 256), FormatPNG)
	direct, err := src.BlurHash(4, 3)
	if err != nil {
		t.Fatalf("direct BlurHash: %v", err)
	}
	res, err := (VariantSet{BlurHashX: 4, BlurHashY: 3}).Process(src)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if res.BlurHash != direct {
		t.Errorf("VariantSet.Process hash %q differs from direct BlurHash %q", res.BlurHash, direct)
	}
}

func TestProcessToReleasesIntermediatesBetweenVariants(t *testing.T) {
	if testing.Short() {
		t.Skip("memory test")
	}
	src := FromImage(gradient(1024, 1024), FormatPNG)

	// First pass: one variant — baseline retained size.
	runtime.GC()
	var b1 runtime.MemStats
	runtime.ReadMemStats(&b1)
	_, err := VariantSet{
		Variants: []Variant{{Width: 512, Format: FormatPNG, Suffix: "a"}},
	}.ProcessTo(src, func(h VariantHeader, r io.Reader) error {
		_, _ = io.ReadAll(r)
		return nil
	})
	if err != nil {
		t.Fatalf("ProcessTo single: %v", err)
	}

	// Second pass: 5 variants — peak retained should be roughly the
	// same as one-variant if intermediates are released.
	runtime.GC()
	var beforeMulti, afterMulti runtime.MemStats
	runtime.ReadMemStats(&beforeMulti)
	_, err = VariantSet{
		Variants: []Variant{
			{Width: 1024, Format: FormatPNG, Suffix: "a"},
			{Width: 800, Format: FormatPNG, Suffix: "b"},
			{Width: 600, Format: FormatPNG, Suffix: "c"},
			{Width: 400, Format: FormatPNG, Suffix: "d"},
			{Width: 200, Format: FormatPNG, Suffix: "e"},
		},
	}.ProcessTo(src, func(h VariantHeader, r io.Reader) error {
		_, _ = io.ReadAll(r)
		// Force GC mid-stream so retained intermediates would show.
		runtime.GC()
		return nil
	})
	runtime.ReadMemStats(&afterMulti)
	if err != nil {
		t.Fatalf("ProcessTo multi: %v", err)
	}
	// Compare in-use memory after each call. The 5-variant call must
	// not retain a multiple of the per-variant memory.
	growth := int64(afterMulti.HeapInuse) - int64(beforeMulti.HeapInuse)
	const oneVariantBytes = 1024 * 1024 * 4 // ~one RGBA scratch
	if growth > 3*oneVariantBytes {
		t.Errorf("ProcessTo retained %d bytes across 5 variants; should be ~one variant", growth)
	}
}

func TestProcessToDoesNotUpscaleByDefault(t *testing.T) {
	src := FromImage(solidRGBA(16, 16, color.RGBA{R: 10, G: 20, B: 30, A: 255}), FormatPNG)
	var widths []int
	_, err := VariantSet{
		Variants: []Variant{{Width: 2048, Format: FormatPNG, Suffix: "big"}},
	}.ProcessTo(src, func(h VariantHeader, r io.Reader) error {
		widths = append(widths, h.Width)
		_, _ = io.ReadAll(r)
		return nil
	})
	if err != nil {
		t.Fatalf("ProcessTo: %v", err)
	}
	if len(widths) != 1 || widths[0] != 16 {
		t.Errorf("default should clamp to source 16, got %v", widths)
	}
}

// TestProcessToAllowUpscaleOptsBackIn confirms the flag works
// symmetrically across both code paths.
func TestProcessToAllowUpscaleOptsBackIn(t *testing.T) {
	src := FromImage(solidRGBA(16, 16, color.RGBA{R: 10, G: 20, B: 30, A: 255}), FormatPNG)
	var widths []int
	_, err := VariantSet{
		AllowUpscale: true,
		Variants:     []Variant{{Width: 64, Format: FormatPNG, Suffix: "big"}},
	}.ProcessTo(src, func(h VariantHeader, r io.Reader) error {
		widths = append(widths, h.Width)
		_, _ = io.ReadAll(r)
		return nil
	})
	if err != nil {
		t.Fatalf("ProcessTo: %v", err)
	}
	if widths[0] != 64 {
		t.Errorf("AllowUpscale should yield 64, got %d", widths[0])
	}
}

// TestOneShotReaderImplementsWriterTo asserts the sink reader exposes
// io.WriterTo so io.Copy doesn't fall back to the 32 KB Read loop.
// storage.Save uses io.Copy under the hood; without WriterTo every
// variant pays an extra buffer-copy hop.
func TestOneShotReaderImplementsWriterTo(t *testing.T) {
	src := FromImage(gradient(64, 48), FormatPNG)
	var checked bool
	_, err := (VariantSet{
		Variants: []Variant{{Width: 32, Format: FormatPNG, Suffix: "a"}},
	}).ProcessTo(src, func(h VariantHeader, r io.Reader) error {
		if _, ok := r.(io.WriterTo); !ok {
			t.Errorf("sink reader does not implement io.WriterTo")
		}
		checked = true
		_, _ = io.Copy(io.Discard, r)
		return nil
	})
	if err != nil {
		t.Fatalf("ProcessTo: %v", err)
	}
	if !checked {
		t.Fatal("sink not invoked")
	}
}

func TestProcessToSinkReaderInvalidAfterReturn(t *testing.T) {
	src := FromImage(gradient(64, 48), FormatPNG)
	set := VariantSet{
		Variants: []Variant{
			{Width: 32, Format: FormatPNG, Suffix: "a"},
			{Width: 16, Format: FormatPNG, Suffix: "b"},
		},
	}
	var stashed io.Reader
	_, err := set.ProcessTo(src, func(h VariantHeader, r io.Reader) error {
		if stashed == nil {
			stashed = r
		}
		// Don't drain — just return.
		return nil
	})
	if err != nil {
		t.Fatalf("ProcessTo: %v", err)
	}
	// Reading the stashed reader after ProcessTo returned must NOT
	// yield the next variant's bytes. It should error (one-shot) or
	// return EOF cleanly with empty data — never a different variant.
	if stashed == nil {
		t.Fatal("did not capture sink reader")
	}
	leaked, _ := io.ReadAll(stashed)
	if len(leaked) > 0 {
		t.Errorf("sink reader leaked %d bytes after ProcessTo returned", len(leaked))
	}
}

// TestVariantSetDoesNotUpscaleByDefault asserts that a 16×16 source
// with a Variant requesting Width: 2048 produces output capped at
// the source's width — silent 100× upscaling is a foot-gun, not a
// feature. Opt back in via VariantSet.AllowUpscale.
func TestVariantSetDoesNotUpscaleByDefault(t *testing.T) {
	src := FromImage(solidRGBA(16, 16, color.RGBA{R: 10, G: 20, B: 30, A: 255}), FormatPNG)
	res, err := VariantSet{
		Variants: []Variant{
			{Width: 2048, Format: FormatPNG, Suffix: "huge"},
		},
	}.Process(src)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if got := res.Variants[0].Width; got != 16 {
		t.Errorf("default should clamp to source width 16, got %d", got)
	}
}

// TestVariantSetAllowUpscaleOptsBackIn confirms the explicit
// AllowUpscale flag restores the old behaviour for callers who
// know what they're doing (e.g., upsampling a vector-rendered tile).
func TestVariantSetAllowUpscaleOptsBackIn(t *testing.T) {
	src := FromImage(solidRGBA(16, 16, color.RGBA{R: 10, G: 20, B: 30, A: 255}), FormatPNG)
	res, err := VariantSet{
		AllowUpscale: true,
		Variants: []Variant{
			{Width: 64, Format: FormatPNG, Suffix: "big"},
		},
	}.Process(src)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if got := res.Variants[0].Width; got != 64 {
		t.Errorf("AllowUpscale should yield Width 64, got %d", got)
	}
}

func TestVariantSetPreservesAspect(t *testing.T) {
	// 400×300 source resized to width=80 with FitInside → height = 60.
	src := FromImage(gradient(400, 300), FormatPNG)
	res, err := VariantSet{
		Variants: []Variant{{Width: 80, Format: FormatPNG}},
	}.Process(src)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if res.Variants[0].Height != 60 {
		t.Errorf("variant height = %d, want 60 (aspect preserved)", res.Variants[0].Height)
	}
}
