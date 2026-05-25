package main

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// TestE2E_ImagePipeline_RendersAllOperations navigates to the image
// pipeline demo and asserts that every transformation produced a
// rendered <img> with an inline data: URL plus a non-empty BlurHash
// string. The demo source is a synthetic gradient so the page renders
// identically on every CI run.
func TestE2E_ImagePipeline_RendersAllOperations(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	sink := &consoleSink{}
	listenConsoleErrors(ctx, sink)

	var got map[string]any
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/framework-ui/image-pipeline"),
		pageReady(),
		chromedp.Evaluate(`(() => {
            const ids = [
                'img-pipeline-source',
                'img-pipeline-resize',
                'img-pipeline-rotate',
                'img-pipeline-flip',
                'img-pipeline-flop',
                'img-pipeline-brightness',
                'img-pipeline-saturation',
                'img-pipeline-placeholder',
            ];
            const out = {};
            for (const id of ids) {
                const el = document.querySelector('[data-test="' + id + '"]');
                if (!el) { return {error: "missing " + id}; }
                if (el.tagName === 'IMG') {
                    out[id + ":prefix"] = (el.getAttribute('src') || '').slice(0, 22);
                    out[id + ":width"]  = el.naturalWidth || 0;
                    out[id + ":height"] = el.naturalHeight || 0;
                } else {
                    out[id + ":text"] = (el.textContent || '').trim();
                }
            }
            const hash = document.querySelector('[data-test="img-pipeline-blurhash"]');
            out["blurhash"] = hash ? (hash.textContent || '').trim() : '';
            out["sourceFormat"] = document.querySelector('[data-test="img-pipeline-grid"]')
                ?.getAttribute('data-source-format') || '';
            return out;
        })()`, &got),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if e, ok := got["error"].(string); ok && e != "" {
		t.Fatalf("setup: %s", e)
	}

	imageTargets := []string{
		"img-pipeline-source",
		"img-pipeline-resize",
		"img-pipeline-rotate",
		"img-pipeline-flip",
		"img-pipeline-flop",
		"img-pipeline-brightness",
		"img-pipeline-saturation",
	}
	for _, id := range imageTargets {
		prefix, _ := got[id+":prefix"].(string)
		if !strings.HasPrefix(prefix, "data:image/png;base64,") {
			t.Errorf("%s src should be a PNG data URL, got prefix %q", id, prefix)
		}
		if w, _ := got[id+":width"].(float64); w == 0 {
			t.Errorf("%s did not load (naturalWidth = 0)", id)
		}
	}

	if prefix, _ := got["img-pipeline-placeholder:prefix"].(string); !strings.HasPrefix(prefix, "data:image/jpeg;base64") {
		t.Errorf("placeholder should be JPEG, got prefix %q", prefix)
	}

	hash, _ := got["blurhash"].(string)
	if len(hash) < 6 {
		t.Errorf("BlurHash should be at least 6 chars, got %q", hash)
	}
	for _, c := range hash {
		if !strings.ContainsRune(
			"0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz#$%*+,-.:;=?@[]^_{|}~",
			c) {
			t.Errorf("BlurHash contains non-base83 char %q in %q", c, hash)
			break
		}
	}

	if fmtName, _ := got["sourceFormat"].(string); fmtName != "png" {
		t.Errorf("source format attr = %q, want png", fmtName)
	}

	if errs := sink.hasErrors(); len(errs) > 0 {
		t.Errorf("console errors on /framework-ui/image-pipeline:\n%s", strings.Join(errs, "\n"))
	}
}

// TestE2E_ImagePipeline_VariantSetRendersPipelineImage asserts that
// VariantSet → PipelineImage produces a multi-format <picture> with
// per-MIME-type <source> elements, the typed variant summary, and a
// placeholder attribute on the fallback <img>.
func TestE2E_ImagePipeline_VariantSetRendersPipelineImage(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var got map[string]any
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/framework-ui/image-pipeline"),
		pageReady(),
		chromedp.Evaluate(`(() => {
            const variant = document.querySelector('[data-test="img-pipeline-variant"]');
            if (!variant) return {error: "missing variant section"};
            const picture = variant.querySelector('picture');
            if (!picture) return {error: "missing <picture>"};
            const sources = Array.from(picture.querySelectorAll('source'));
            const img = picture.querySelector('img');
            const list = document.querySelector('[data-test="img-pipeline-variant-list"]');
            return {
                sourceCount:   sources.length,
                sourceTypes:   sources.map(s => s.getAttribute('type')).join(','),
                hasJPEG:       sources.some(s => s.getAttribute('type') === 'image/jpeg') ? "yes" : "no",
                hasWebP:       sources.some(s => s.getAttribute('type') === 'image/webp') ? "yes" : "no",
                firstType:     sources[0] ? sources[0].getAttribute('type') : '',
                fallbackSrc:   (img && img.getAttribute('src') || '').slice(0, 22),
                fallbackWidth: img ? img.getAttribute('width') : '',
                placeholder:   (img && img.getAttribute('data-placeholder') || '').slice(0, 22),
                listItems:     list ? list.children.length : 0,
            };
        })()`, &got),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if e, ok := got["error"].(string); ok && e != "" {
		t.Fatalf("setup: %s", e)
	}
	if got["hasJPEG"] != "yes" {
		t.Errorf("missing image/jpeg <source>; types = %q", got["sourceTypes"])
	}
	if got["hasWebP"] != "yes" {
		t.Errorf("missing image/webp <source>; types = %q", got["sourceTypes"])
	}
	// WebP must come before JPEG so legacy browsers fall through.
	if got["firstType"] != "image/webp" {
		t.Errorf("first <source type> = %q, want image/webp", got["firstType"])
	}
	if pref, _ := got["fallbackSrc"].(string); !strings.HasPrefix(pref, "data:image/jpeg;base64") {
		t.Errorf("fallback src should be JPEG data URL, got prefix %q", pref)
	}
	if got["fallbackWidth"] != "160" {
		t.Errorf("fallback width = %v, want 160", got["fallbackWidth"])
	}
	if pref, _ := got["placeholder"].(string); !strings.HasPrefix(pref, "data:image/jpeg;base64") {
		t.Errorf("placeholder should be JPEG data URL, got prefix %q", pref)
	}
	if n, _ := got["listItems"].(float64); int(n) != 4 {
		t.Errorf("variant list count = %v, want 4 (2 JPEG + 2 PNG)", n)
	}
}

// TestE2E_ImagePipeline_RotateChangesAspect asserts that the rotated
// image is wider-than-tall flipped compared to the source — a quick
// sanity check on the pipeline actually applying the transformation
// rather than just embedding the same data URL.
func TestE2E_ImagePipeline_RotateChangesAspect(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var got map[string]float64
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/framework-ui/image-pipeline"),
		pageReady(),
		chromedp.Evaluate(`(() => {
            const src = document.querySelector('[data-test="img-pipeline-source"]');
            const rot = document.querySelector('[data-test="img-pipeline-rotate"]');
            return {
                srcW: src?.naturalWidth || 0,  srcH: src?.naturalHeight || 0,
                rotW: rot?.naturalWidth || 0,  rotH: rot?.naturalHeight || 0,
            };
        })()`, &got),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if got["srcW"] != got["rotH"] || got["srcH"] != got["rotW"] {
		t.Errorf("rotated dims should be source dims swapped; got src=%vx%v rot=%vx%v",
			got["srcW"], got["srcH"], got["rotW"], got["rotH"])
	}
}
