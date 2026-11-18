package imagefield_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"strings"
	"testing"

	fwimage "github.com/DonaldMurillo/gofastr/framework/image"
	"github.com/DonaldMurillo/gofastr/framework/imagefield"
)

func TestSweepImagefieldNewRefusesTooManyVariants(t *testing.T) {
	vs := make([]fwimage.Variant, fwimage.MaxVariantsPerSet+1)
	for i := range vs {
		vs[i] = fwimage.Variant{Width: i + 1, Format: fwimage.FormatPNG}
	}
	if _, err := imagefield.New(imagefield.Config{Variants: vs}); err == nil {
		t.Fatal("New accepted more than MaxVariantsPerSet variants")
	}
}

func TestSweepImagefieldPassesSetOptionsThrough(t *testing.T) {
	ctx := context.Background()
	webp := []fwimage.Variant{{Width: 64, Format: fwimage.FormatWebP, Suffix: "a"}, {Width: 16, Format: fwimage.FormatWebP, Suffix: "b"}}

	up := imagefield.MustNew(imagefield.Config{Variants: webp, AllowUpscale: true})
	out, err := up.DeriveImage(ctx, newMemStore(), testPNG(t, 32, 32), "x/p.png")
	if err != nil || len(out.Variants) != 2 || out.Variants[0].Width != 16 || out.Variants[1].Width != 64 {
		t.Fatalf("AllowUpscale: %+v, %v; want widths 16 then 64", out, err)
	}
	if !strings.HasSuffix(out.Variants[0].StorageRef, "x/p-b.webp") {
		t.Fatalf("rendition key %q, want x/p-b.webp", out.Variants[0].StorageRef)
	}

	capped := imagefield.MustNew(imagefield.Config{Variants: webp, MaxPixels: 100})
	if _, err := capped.DeriveImage(ctx, newMemStore(), testPNG(t, 32, 32), "x/p.png"); !errors.Is(err, fwimage.ErrDecompressionBomb) {
		t.Fatalf("MaxPixels 100 on a 32x32 upload: %v, want ErrDecompressionBomb", err)
	}

	var anim bytes.Buffer
	pal := color.Palette{color.Black, color.White}
	frames := &gif.GIF{Image: []*image.Paletted{image.NewPaletted(image.Rect(0, 0, 4, 4), pal), image.NewPaletted(image.Rect(0, 0, 4, 4), pal)}, Delay: []int{1, 1}}
	if err := gif.EncodeAll(&anim, frames); err != nil {
		t.Fatal(err)
	}
	strict := imagefield.MustNew(imagefield.Config{Variants: webp, RejectAnimated: true})
	if _, err := strict.DeriveImage(ctx, newMemStore(), anim.Bytes(), "x/a.gif"); !errors.Is(err, fwimage.ErrAnimatedSource) {
		t.Fatalf("RejectAnimated on a two-frame GIF: %v, want ErrAnimatedSource", err)
	}
}
