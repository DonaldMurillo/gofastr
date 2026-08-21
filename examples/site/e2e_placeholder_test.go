package main

import (
	"bytes"
	"context"
	"fmt"
	"image/png"
	"testing"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

type placeholderGeometry struct {
	Found bool `json:"found"`
	// NaturalWidth is non-zero only if the browser actually decoded the
	// inline data URI. This is the assertion the old data-placeholder
	// attribute could never satisfy: an attribute nothing consumes always
	// passes a markup test while painting nothing.
	NaturalWidth  int     `json:"naturalWidth"`
	NaturalHeight int     `json:"naturalHeight"`
	Width         float64 `json:"width"`
	Height        float64 `json:"height"`
	// Delta* compare the placeholder's box to the real image's. A
	// placeholder covering a different box is either invisible or bleeding
	// outside the image it stands in for.
	DeltaLeft float64 `json:"deltaLeft"`
	DeltaTop  float64 `json:"deltaTop"`
	DeltaW    float64 `json:"deltaW"`
	DeltaH    float64 `json:"deltaH"`
	// TopMostIsRealImage records what a user actually sees at the centre of
	// the frame, the real image has to be in front.
	TopMostIsRealImage bool   `json:"topMostIsRealImage"`
	TopMostClass       string `json:"topMostClass"`
	Position           string `json:"position"`
	Lazy               bool   `json:"lazy"`
	Decoding           string `json:"decoding"`
}

// placeholderProbe inspects the PipelineImage instance on the showcase page,
// the one with a placeholder stacked behind a real image.
const placeholderProbe = `(() => {
  const lqip = document.querySelector('.ui-image--placeheld .ui-image__lqip');
  if (!lqip) return {found: false};
  const root = lqip.closest('[data-fui-comp="ui-image"]');
  const real = root && root.querySelector('.ui-image__img');
  const a = lqip.getBoundingClientRect();
  const b = real ? real.getBoundingClientRect() : a;
  const cs = getComputedStyle(lqip);
  const mid = document.elementFromPoint(b.left + b.width / 2, b.top + b.height / 2);
  return {
    found: true,
    naturalWidth: lqip.naturalWidth,
    naturalHeight: lqip.naturalHeight,
    width: a.width,
    height: a.height,
    deltaLeft: Math.abs(a.left - b.left),
    deltaTop: Math.abs(a.top - b.top),
    deltaW: Math.abs(a.width - b.width),
    deltaH: Math.abs(a.height - b.height),
    topMostIsRealImage: mid === real,
    topMostClass: mid ? String(mid.className) : '',
    position: cs.position,
    lazy: lqip.getAttribute('loading') === 'lazy',
    decoding: lqip.getAttribute('decoding') || '',
  };
})()`

// hideRealImage makes the real image transparent so a screenshot captures
// what the placeholder alone puts on screen. Done in the browser at runtime
// rather than by shipping a test-only CSS hook.
const hideRealImage = `(() => {
  const img = document.querySelector('.ui-image--placeheld .ui-image__img');
  if (!img) return false;
  img.style.opacity = '0';
  return true;
})()`

// TestE2EPlaceholderPaints is the pixels-not-probes check for the image
// placeholder. It verifies the browser decoded the inline placeholder, that it
// covers the real image's box, that the real image is the one in front, and
// that with the real image hidden the pixels left on screen are a colourful
// blur rather than the component's flat resting grey.
//
// The predecessor tests asserted only that a data-placeholder attribute was
// present in the markup, which stayed green for as long as the feature was
// entirely inert.
func TestE2EPlaceholderPaints(t *testing.T) {
	base := siteE2EServer(t)
	ctx := siteBrowserCtx(t)

	var geo placeholderGeometry
	var hidden bool
	var shot []byte
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/pipelineimage"),
		chromedp.WaitVisible(".ui-image__lqip", chromedp.ByQuery),
		chromedp.Evaluate(placeholderProbe, &geo),
		chromedp.Evaluate(hideRealImage, &hidden),
		chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			shot, err = page.CaptureScreenshot().WithFromSurface(true).Do(ctx)
			return err
		}),
	); err != nil {
		t.Fatalf("placeholder probe: %v", err)
	}

	if !geo.Found {
		t.Fatal("no placeheld .ui-image__lqip on /components/pipelineimage — the demo regressed")
	}
	if geo.NaturalWidth == 0 || geo.NaturalHeight == 0 {
		t.Errorf("browser did not decode the placeholder (natural size %dx%d) — the data URI is not a valid image",
			geo.NaturalWidth, geo.NaturalHeight)
	}
	if geo.Width < 1 || geo.Height < 1 {
		t.Errorf("placeholder has no rendered box: %.1fx%.1f", geo.Width, geo.Height)
	}
	if geo.Position != "absolute" {
		t.Errorf("placeholder position = %q, want absolute so it stacks behind", geo.Position)
	}
	// A data URI costs no request, so deferring or async-decoding it could
	// only make the placeholder appear after the image it stands in for.
	if geo.Lazy {
		t.Error("placeholder must not be lazy-loaded")
	}
	if geo.Decoding != "sync" {
		t.Errorf("placeholder decoding = %q, want sync", geo.Decoding)
	}
	// One pixel of tolerance for subpixel layout rounding.
	for _, d := range []struct {
		name string
		val  float64
	}{
		{"left", geo.DeltaLeft}, {"top", geo.DeltaTop},
		{"width", geo.DeltaW}, {"height", geo.DeltaH},
	} {
		if d.val > 1 {
			t.Errorf("placeholder %s differs from the real image by %.1fpx; it must cover the same box", d.name, d.val)
		}
	}
	if !geo.TopMostIsRealImage {
		t.Errorf("the placeholder is painting over the real image (top element at centre: %q)", geo.TopMostClass)
	}
	if !hidden {
		t.Fatal("could not hide the real image for the pixel check")
	}
	if err := screenshotIsColourful(shot); err != nil {
		t.Errorf("placeholder pixels: %v", err)
	}
}

// screenshotIsColourful reports an error unless some pixel is meaningfully
// saturated. With the real image hidden, an all-grey screenshot means the
// decoded blur never painted, exactly the pre-fix behaviour, where the
// component fell back to --color-surface-soft.
func screenshotIsColourful(data []byte) error {
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("decode screenshot: %w", err)
	}
	bounds := img.Bounds()
	best := 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y += 2 {
		for x := bounds.Min.X; x < bounds.Max.X; x += 2 {
			r, g, b, _ := img.At(x, y).RGBA()
			r8, g8, b8 := int(r>>8), int(g>>8), int(b>>8)
			hi, lo := r8, r8
			for _, v := range []int{g8, b8} {
				if v > hi {
					hi = v
				}
				if v < lo {
					lo = v
				}
			}
			if hi-lo > best {
				best = hi - lo
			}
		}
	}
	// The demo mockup uses saturated indigo/amber/teal blocks, so its blur
	// has a wide channel spread. 30 sits far above the ~0-4 spread of the
	// grey resting fill and well below what the real mockup produces.
	if best < 30 {
		return fmt.Errorf("no saturated pixels found (max channel spread %d); the blur did not paint", best)
	}
	return nil
}
