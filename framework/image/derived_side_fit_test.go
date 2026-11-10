package image_test

import (
	"bytes"
	"encoding/base64"
	stdimage "image"
	"image/jpeg"
	"strings"
	"testing"

	frameworkimage "github.com/DonaldMurillo/gofastr/framework/image"
)

func blankSource(width, height int) *frameworkimage.Image {
	return frameworkimage.FromImage(stdimage.NewRGBA(stdimage.Rect(0, 0, width, height)), frameworkimage.FormatPNG)
}

var everyFit = []struct {
	name string
	fit  frameworkimage.Fit
}{
	{"fill", frameworkimage.FitFill},
	{"inside", frameworkimage.FitInside},
	{"outside", frameworkimage.FitOutside},
}

// TestDerivedSideKeepsGivenWidth is C07: with height derived from width
// 16, a 1920x36 banner truncated its height to 0, FitInside's scale went
// to zero and the result was 1x1. 1000x333 lost a pixel of width to the
// same truncation (15x5). The 16:1 and 4:1 sources are the controls the
// bug never touched.
func TestDerivedSideKeepsGivenWidth(t *testing.T) {
	cases := []struct{ sourceWidth, sourceHeight, width, height, wantWidth, wantHeight int }{
		{1920, 36, 16, 0, 16, 1},
		{2000, 3, 16, 0, 16, 1},
		{1000, 333, 16, 0, 16, 5},
		{2000, 125, 16, 0, 16, 1},
		{640, 160, 16, 0, 16, 4},
		{36, 1920, 0, 16, 1, 16},
		{1920, 36, 0, 4, 213, 4},
		{100, 50, 400, 0, 400, 200},
	}
	for _, testCase := range cases {
		source := blankSource(testCase.sourceWidth, testCase.sourceHeight)
		for _, mode := range everyFit {
			bounds := source.Resize(testCase.width, testCase.height, frameworkimage.WithFit(mode.fit)).Bounds()
			if bounds.Dx() != testCase.wantWidth || bounds.Dy() != testCase.wantHeight {
				t.Errorf("%dx%d Resize(%d, %d) %s = %dx%d, want %dx%d",
					testCase.sourceWidth, testCase.sourceHeight, testCase.width, testCase.height, mode.name,
					bounds.Dx(), bounds.Dy(), testCase.wantWidth, testCase.wantHeight)
			}
		}
	}
}

// TestGivenBoxStillFits is the control for a box with both sides given:
// FitInside and FitOutside still scale the source into and over it.
func TestGivenBoxStillFits(t *testing.T) {
	cases := []struct {
		fit                   frameworkimage.Fit
		wantWidth, wantHeight int
	}{
		{frameworkimage.FitInside, 100, 25},
		{frameworkimage.FitOutside, 400, 100},
		{frameworkimage.FitFill, 100, 100},
	}
	source := blankSource(640, 160)
	for _, testCase := range cases {
		bounds := source.Resize(100, 100, frameworkimage.WithFit(testCase.fit)).Bounds()
		if bounds.Dx() != testCase.wantWidth || bounds.Dy() != testCase.wantHeight {
			t.Errorf("Resize(100, 100) fit %d = %dx%d, want %dx%d", testCase.fit, bounds.Dx(), bounds.Dy(), testCase.wantWidth, testCase.wantHeight)
		}
	}
	if bounds := blankSource(100, 50).Resize(400, 0, frameworkimage.WithoutEnlargement()).Bounds(); bounds.Dx() != 100 || bounds.Dy() != 50 {
		t.Errorf("WithoutEnlargement upscale = %dx%d, want the 100x50 source", bounds.Dx(), bounds.Dy())
	}
}

// TestPlaceholderKeepsWidthOnBanner: the encoded placeholder of a wide
// banner decodes at the default width 16, not 1x1.
func TestPlaceholderKeepsWidthOnBanner(t *testing.T) {
	cases := []struct{ sourceWidth, sourceHeight, wantWidth, wantHeight int }{
		{1920, 36, 16, 1},
		{2000, 3, 16, 1},
		{640, 160, 16, 4},
	}
	for _, testCase := range cases {
		placeholder, err := blankSource(testCase.sourceWidth, testCase.sourceHeight).Placeholder()
		if err != nil {
			t.Fatal(err)
		}
		_, payload, found := strings.Cut(placeholder, ",")
		if !found {
			t.Fatalf("placeholder %q is not a data URL", placeholder)
		}
		data, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := jpeg.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		if decoded.Width != testCase.wantWidth || decoded.Height != testCase.wantHeight {
			t.Errorf("%dx%d placeholder = %dx%d, want %dx%d", testCase.sourceWidth, testCase.sourceHeight,
				decoded.Width, decoded.Height, testCase.wantWidth, testCase.wantHeight)
		}
	}
}

// TestFitKeepsBindingSideExact: with both sides given, the side the fit
// binds on is the one asked for. The scale went through a float, and
// 22*(15/22) is 14.999999999999998: a 22x22 source fitted inside 15x100
// came out 14x14, and outside 100x15 came out 14x14 too, under the box
// it must cover.
func TestFitKeepsBindingSideExact(t *testing.T) {
	cases := []struct {
		sourceWidth, sourceHeight, width, height int
		fit                                      frameworkimage.Fit
		wantWidth, wantHeight                    int
	}{
		{22, 22, 15, 100, frameworkimage.FitInside, 15, 15},
		{22, 22, 100, 15, frameworkimage.FitInside, 15, 15},
		{44, 22, 30, 100, frameworkimage.FitInside, 30, 15},
		{22, 22, 15, 1, frameworkimage.FitOutside, 15, 15},
		{22, 22, 1, 15, frameworkimage.FitOutside, 15, 15},
		{39, 39, 31, 31, frameworkimage.FitInside, 31, 31},
		{39, 39, 31, 31, frameworkimage.FitOutside, 31, 31},
	}
	for _, testCase := range cases {
		bounds := blankSource(testCase.sourceWidth, testCase.sourceHeight).
			Resize(testCase.width, testCase.height, frameworkimage.WithFit(testCase.fit)).Bounds()
		if bounds.Dx() != testCase.wantWidth || bounds.Dy() != testCase.wantHeight {
			t.Errorf("%dx%d Resize(%d, %d) fit %d = %dx%d, want %dx%d",
				testCase.sourceWidth, testCase.sourceHeight, testCase.width, testCase.height, testCase.fit,
				bounds.Dx(), bounds.Dy(), testCase.wantWidth, testCase.wantHeight)
		}
	}
}
