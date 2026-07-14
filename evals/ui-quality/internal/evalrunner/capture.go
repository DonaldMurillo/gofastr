package evalrunner

import (
	"bytes"
	"context"
	"fmt"
	"image/png"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/axetest"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/device"
)

const (
	screenshotViewport = "viewport"
	screenshotFullPage = "full-page"
)

type browserLayoutAudit struct {
	InnerWidth         float64             `json:"innerWidth"`
	ScrollWidth        float64             `json:"scrollWidth"`
	DocumentHeight     float64             `json:"documentHeight"`
	DeviceScaleFactor  float64             `json:"deviceScaleFactor"`
	TouchPoints        int                 `json:"touchPoints"`
	Orientation        string              `json:"orientation"`
	UserAgent          string              `json:"userAgent"`
	ColorScheme        string              `json:"colorScheme"`
	ColorSchemeMeta    string              `json:"colorSchemeMeta"`
	Violations         []BoundsViolation   `json:"violations"`
	ContrastViolations []ContrastViolation `json:"contrastViolations"`
}

type candidateNetworkGuard struct {
	base    *url.URL
	mu      sync.Mutex
	blocked map[string]bool
}

func newCandidateNetworkGuard(baseURL string) (*candidateNetworkGuard, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return nil, err
	}
	if base.Scheme == "" || base.Host == "" {
		return nil, fmt.Errorf("candidate base URL is not absolute: %q", baseURL)
	}
	return &candidateNetworkGuard{base: base, blocked: make(map[string]bool)}, nil
}

func (g *candidateNetworkGuard) allows(rawURL string) bool {
	requestURL, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	switch strings.ToLower(requestURL.Scheme) {
	case "about", "blob", "data":
		return true
	case "http", "https":
		return strings.EqualFold(requestURL.Scheme, g.base.Scheme) && strings.EqualFold(requestURL.Host, g.base.Host)
	default:
		return false
	}
}

func (g *candidateNetworkGuard) record(rawURL string) {
	g.mu.Lock()
	g.blocked[rawURL] = true
	g.mu.Unlock()
}

func (g *candidateNetworkGuard) blockedURLs() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	urls := make([]string, 0, len(g.blocked))
	for rawURL := range g.blocked {
		urls = append(urls, rawURL)
	}
	sort.Strings(urls)
	return urls
}

func installCandidateNetworkGuard(ctx context.Context, guard *candidateNetworkGuard) error {
	if err := chromedp.Run(ctx, fetch.Enable().WithPatterns([]*fetch.RequestPattern{{URLPattern: "*"}})); err != nil {
		return err
	}
	chromedp.ListenTarget(ctx, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok || paused.Request == nil {
			return
		}
		go func() {
			target := chromedp.FromContext(ctx)
			if target == nil || target.Target == nil {
				return
			}
			executorCtx := cdp.WithExecutor(ctx, target.Target)
			if guard.allows(paused.Request.URL) {
				_ = fetch.ContinueRequest(paused.RequestID).Do(executorCtx)
				return
			}
			guard.record(paused.Request.URL)
			_ = fetch.FailRequest(paused.RequestID, network.ErrorReasonBlockedByClient).Do(executorCtx)
		}()
	})
	return nil
}

func uniqueSorted(values []string) []string {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			set[value] = true
		}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func assetFailureList(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strings.Split(value, "\n")
}

func awaitEvaluationPromise(params *cdpruntime.EvaluateParams) *cdpruntime.EvaluateParams {
	params.AwaitPromise = true
	return params
}

const browserLayoutAuditJS = `(() => {
	const tolerance = 1;
	const viewportWidth = window.innerWidth;
	const root = document.documentElement;
	const clippedByScrollableAncestor = (el, rect) => {
		for (let parent = el.parentElement; parent && parent !== document.body; parent = parent.parentElement) {
			const style = getComputedStyle(parent);
			if (["auto", "scroll"].includes(style.overflowX) && parent.scrollWidth > parent.clientWidth + tolerance) {
				const parentRect = parent.getBoundingClientRect();
				if (rect.left < parentRect.left - tolerance || rect.right > parentRect.right + tolerance) return true;
			}
		}
		return false;
	};
	const describe = (el) => {
		let value = el.tagName.toLowerCase();
		if (el.id) return value + "#" + el.id;
		const classes = Array.from(el.classList || []).slice(0, 2);
		if (classes.length) value += "." + classes.join(".");
		return value;
	};
	const parseColor = value => {
		const match = value && value.match(/^rgba?\(([^)]+)\)$/i);
		if (!match) return null;
		const parts = match[1].split(/[\s,\/]+/).filter(Boolean).map(Number);
		if (parts.length < 3 || parts.some(Number.isNaN)) return null;
		return {r: parts[0], g: parts[1], b: parts[2], a: parts.length > 3 ? parts[3] : 1};
	};
	const composite = (front, back) => {
		const a = front.a + back.a * (1 - front.a);
		if (a <= 0) return {r: 0, g: 0, b: 0, a: 0};
		return {
			r: (front.r * front.a + back.r * back.a * (1 - front.a)) / a,
			g: (front.g * front.a + back.g * back.a * (1 - front.a)) / a,
			b: (front.b * front.a + back.b * back.a * (1 - front.a)) / a,
			a
		};
	};
	const effectiveBackground = el => {
		let effective = {r: 0, g: 0, b: 0, a: 0};
		for (let current = el; current; current = current.parentElement) {
			const style = getComputedStyle(current);
			if (style.backgroundImage !== "none" || style.mixBlendMode !== "normal") return null;
			const layer = parseColor(style.backgroundColor);
			if (layer) effective = composite(effective, layer);
			if (effective.a >= 0.999) return effective;
		}
		return composite(effective, {r: 255, g: 255, b: 255, a: 1});
	};
	const luminance = color => {
		const channel = value => {
			value /= 255;
			return value <= 0.04045 ? value / 12.92 : Math.pow((value + 0.055) / 1.055, 2.4);
		};
		return 0.2126 * channel(color.r) + 0.7152 * channel(color.g) + 0.0722 * channel(color.b);
	};
	const colorString = color => "rgb(" + Math.round(color.r) + ", " + Math.round(color.g) + ", " + Math.round(color.b) + ")";
	const directText = el => Array.from(el.childNodes).some(node => node.nodeType === Node.TEXT_NODE && node.textContent.trim());
	const labeledTextElements = control => {
		if (control.matches('input[type="button"], input[type="submit"]')) {
			return (control.value || "").trim() ? [control] : [];
		}
		return [control, ...control.querySelectorAll("*")].filter(el => directText(el) && !isVisuallySuppressed(el));
	};
	const isVisuallySuppressed = (el) => {
		for (let current = el; current && current !== document.body; current = current.parentElement) {
			if (current.hidden) return true;
			const style = getComputedStyle(current);
			if (style.display === "none" || style.visibility === "hidden" || Number(style.opacity) === 0) return true;
		}
		return false;
	};
	const violations = [];
	for (const el of document.querySelectorAll("body *")) {
		const closedDetails = el.closest("details:not([open])");
		if (closedDetails && !el.closest("summary")) continue;
		if (isVisuallySuppressed(el)) continue;
		const ariaHidden = el.closest('[aria-hidden="true"]');
		const interactive = el.matches('a, button, input, select, textarea, [role="button"], [tabindex]') || !!el.querySelector('a, button, input, select, textarea, [role="button"], [tabindex]');
		if (ariaHidden && !interactive && !(el.textContent || "").trim()) continue;
		const rect = el.getBoundingClientRect();
		if (rect.width <= 1 || rect.height <= 1) continue;
		if (rect.left >= -tolerance && rect.right <= viewportWidth + tolerance) continue;
		if (clippedByScrollableAncestor(el, rect)) continue;
		violations.push({
			element: describe(el),
			left: Math.round(rect.left * 100) / 100,
			right: Math.round(rect.right * 100) / 100,
			viewport_width: viewportWidth
		});
		if (violations.length >= 12) break;
	}
	const contrastViolations = [];
	const controls = document.querySelectorAll('a, button, summary, [role="button"], input[type="button"], input[type="submit"]');
	for (const control of controls) {
		if (control.disabled || control.getAttribute("aria-disabled") === "true" || isVisuallySuppressed(control)) continue;
		const controlRect = control.getBoundingClientRect();
		if (controlRect.bottom <= 0 || controlRect.right <= 0 || controlRect.left >= viewportWidth) continue;
		const textElements = labeledTextElements(control);
		for (const textElement of textElements) {
			const style = getComputedStyle(textElement);
			const foreground = parseColor(style.color);
			const background = effectiveBackground(textElement);
			if (!foreground || !background) continue;
			const opaqueForeground = composite(foreground, background);
			const foregroundLuminance = luminance(opaqueForeground);
			const backgroundLuminance = luminance(background);
			const ratio = (Math.max(foregroundLuminance, backgroundLuminance) + 0.05) / (Math.min(foregroundLuminance, backgroundLuminance) + 0.05);
			const size = parseFloat(style.fontSize) || 0;
			const weight = Number(style.fontWeight) || 400;
			const minimum = size >= 24 || (size >= 18.66 && weight >= 700) ? 3 : 4.5;
			if (ratio >= minimum) continue;
			contrastViolations.push({
				element: describe(control),
				foreground: colorString(opaqueForeground),
				background: colorString(background),
				ratio: Math.round(ratio * 100) / 100,
				minimum
			});
			break;
		}
		if (contrastViolations.length >= 12) break;
	}
	return {
		innerWidth: viewportWidth,
		scrollWidth: root.scrollWidth,
		documentHeight: Math.max(root.scrollHeight, document.body ? document.body.scrollHeight : 0),
		deviceScaleFactor: window.devicePixelRatio,
		touchPoints: navigator.maxTouchPoints || 0,
		orientation: screen.orientation ? screen.orientation.type : "unknown",
		userAgent: navigator.userAgent,
		colorScheme: root.getAttribute("data-color-scheme") || "",
		colorSchemeMeta: (document.querySelector('meta[name="color-scheme"]') || {}).content || "",
		violations,
		contrastViolations
	};
})()`

// imageSettlingJS is the shared settle-and-report block: wait for every
// image (bounded), recording ones that fail to load/decode into `failures`.
// It expects `delay` and `failures` to be in scope.
const imageSettlingJS = `	const images = Array.from(document.images).filter(image => image.currentSrc || image.getAttribute("src"));
	await Promise.all(images.map(image => new Promise(resolve => {
		if (image.complete) {
			if (!image.naturalWidth) failures.push(image.currentSrc || image.getAttribute("src"));
			if (image.decode) image.decode().catch(() => failures.push(image.currentSrc || image.getAttribute("src"))).finally(resolve); else resolve();
			return;
		}
		let settled = false;
		const done = failed => {
			if (settled) return;
			settled = true;
			if (failed) failures.push(image.currentSrc || image.getAttribute("src"));
			resolve();
		};
		image.addEventListener("load", () => done(false), {once: true});
		image.addEventListener("error", () => done(true), {once: true});
		setTimeout(() => done(!image.complete || !image.naturalWidth), 5000);
	})));
`

const initialAssetSettlingJS = `(async () => {
	const delay = ms => new Promise(resolve => setTimeout(resolve, ms));
	const failures = [];
	if (document.fonts && document.fonts.ready) {
		await Promise.race([document.fonts.ready, delay(5000)]);
	}
` + imageSettlingJS + `	await delay(100);
	return Array.from(new Set(failures)).join("\n");
})()`

const hydrateFullPageAssetsJS = `(async () => {
	const delay = ms => new Promise(resolve => setTimeout(resolve, ms));
	const failures = [];
	const nextPaint = () => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
	const step = Math.max(240, Math.floor(window.innerHeight * 0.75));
	let y = 0;
	for (let pass = 0; pass < 200; pass++) {
		const height = Math.max(document.documentElement.scrollHeight, document.body ? document.body.scrollHeight : 0);
		if (y > height) break;
		window.scrollTo(0, y);
		await nextPaint();
		await delay(75);
		y += step;
	}
	window.scrollTo(0, Math.max(document.documentElement.scrollHeight, document.body ? document.body.scrollHeight : 0));
	await nextPaint();
	await delay(150);
` + imageSettlingJS + `	if (document.fonts && document.fonts.ready) {
		await Promise.race([document.fonts.ready, delay(5000)]);
	}
	window.scrollTo(0, 0);
	await nextPaint();
	await delay(100);
	return Array.from(new Set(failures)).join("\n");
})()`

func captureCandidate(ctx context.Context, baseURL, blindDir string, scenario Scenario, viewports []Viewport) ([]ScreenshotResult, []string) {
	if err := os.MkdirAll(blindDir, 0o755); err != nil {
		return nil, []string{err.Error()}
	}
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		// Mobile browsers use overlay scrollbars. Hiding classic headless-Chrome
		// scrollbars preserves the configured CSS viewport width in the evidence.
		chromedp.Flag("hide-scrollbars", true),
		chromedp.WSURLReadTimeout(90*time.Second),
	)
	alloc, allocCancel := chromedp.NewExecAllocator(ctx, opts...)
	defer allocCancel()
	browser, browserCancel := chromedp.NewContext(alloc)
	defer browserCancel()

	var viewportResults []ScreenshotResult
	var fullPageResults []ScreenshotResult
	var issues []string
	shotIndex := 0
	for _, page := range scenario.Pages {
		for _, vp := range viewports {
			shotIndex++
			url := strings.TrimRight(baseURL, "/") + page.Path
			status := httpStatus(ctx, url)
			var viewportPNG, fullPagePNG []byte
			var initialAssetFailures, fullPageAssetFailures string
			var audit browserLayoutAudit
			// A cancellable chromedp task context also owns its target. Reusing it
			// after cancel closes the shared tab and makes every later shot fail
			// with context canceled. Give each screenshot its own tab instead.
			tab, closeTab := chromedp.NewContext(browser)
			captureCtx, cancel := context.WithTimeout(tab, 45*time.Second)
			networkGuard, guardErr := newCandidateNetworkGuard(baseURL)
			if guardErr == nil {
				guardErr = installCandidateNetworkGuard(captureCtx, networkGuard)
			}
			if guardErr != nil {
				cancel()
				closeTab()
				issues = append(issues, fmt.Sprintf("%s %s: install network guard: %v", page.Name, vp.ID, guardErr))
				continue
			}
			emulationActions := []chromedp.Action{chromedp.EmulateReset()}
			expectedScale := 1.0
			if isMobileViewport(vp) {
				expectedScale = 3
				emulationActions = append(emulationActions,
					emulation.SetUserAgentOverride(device.IPhone13.Device().UserAgent),
					chromedp.EmulateViewport(vp.Width, vp.Height, chromedp.EmulateScale(expectedScale), chromedp.EmulateMobile, chromedp.EmulateTouch, chromedp.EmulatePortrait),
				)
			} else {
				emulationActions = append(emulationActions, chromedp.EmulateViewport(vp.Width, vp.Height, chromedp.EmulateScale(expectedScale), chromedp.EmulateLandscape))
			}
			actions := append(emulationActions,
				chromedp.Navigate(url),
				chromedp.WaitVisible(page.ReadySelector, chromedp.ByQuery),
				axetest.Prepare(vp.Scheme),
				chromedp.Evaluate(initialAssetSettlingJS, &initialAssetFailures, awaitEvaluationPromise),
				chromedp.CaptureScreenshot(&viewportPNG),
				chromedp.Evaluate(hydrateFullPageAssetsJS, &fullPageAssetFailures, awaitEvaluationPromise),
				chromedp.Evaluate(browserLayoutAuditJS, &audit),
				// Quality 100 is the chromedp contract for PNG. Any lower value
				// produces JPEG bytes, regardless of the destination filename.
				chromedp.FullScreenshot(&fullPagePNG, 100),
			)
			err := chromedp.Run(captureCtx, actions...)
			blockedURLs := networkGuard.blockedURLs()
			cancel()
			closeTab()
			if len(blockedURLs) > 0 {
				issues = append(issues, fmt.Sprintf("%s %s: blocked non-candidate network requests: %s", page.Name, vp.ID, strings.Join(blockedURLs, ", ")))
			}
			assetFailures := append(assetFailureList(initialAssetFailures), assetFailureList(fullPageAssetFailures)...)
			if len(assetFailures) > 0 {
				issues = append(issues, fmt.Sprintf("%s %s: image assets failed to load or decode: %s", page.Name, vp.ID, strings.Join(uniqueSorted(assetFailures), ", ")))
			}
			if err != nil {
				issues = append(issues, fmt.Sprintf("%s %s: screenshot failed: %v", page.Name, vp.ID, err))
				continue
			}
			if status < 200 || status >= 300 {
				issues = append(issues, fmt.Sprintf("%s %s: HTTP status %d", page.Name, vp.ID, status))
			}
			overflow := audit.ScrollWidth > audit.InnerWidth+1
			if overflow {
				issues = append(issues, fmt.Sprintf("%s %s: horizontal overflow", page.Name, vp.ID))
			}
			if len(audit.Violations) > 0 {
				issues = append(issues, fmt.Sprintf("%s %s: %d visible element(s) cross viewport bounds", page.Name, vp.ID, len(audit.Violations)))
			}
			if len(audit.ContrastViolations) > 0 {
				issues = append(issues, fmt.Sprintf("%s %s: %d interactive label(s) fail text contrast", page.Name, vp.ID, len(audit.ContrastViolations)))
			}
			if math.Abs(audit.DeviceScaleFactor-expectedScale) > 0.01 {
				issues = append(issues, fmt.Sprintf("%s %s: device scale factor %.2f, want %.2f", page.Name, vp.ID, audit.DeviceScaleFactor, expectedScale))
			}
			if audit.ColorScheme != vp.Scheme {
				issues = append(issues, fmt.Sprintf("%s %s: data-color-scheme is %q, want %q", page.Name, vp.ID, audit.ColorScheme, vp.Scheme))
			}
			if audit.ColorSchemeMeta != vp.Scheme {
				issues = append(issues, fmt.Sprintf("%s %s: color-scheme meta is %q, want %q", page.Name, vp.ID, audit.ColorSchemeMeta, vp.Scheme))
			}
			if isMobileViewport(vp) {
				if audit.TouchPoints < 1 {
					issues = append(issues, fmt.Sprintf("%s %s: touch emulation is disabled", page.Name, vp.ID))
				}
				if !strings.Contains(audit.UserAgent, "Mobile") {
					issues = append(issues, fmt.Sprintf("%s %s: user agent is not mobile", page.Name, vp.ID))
				}
				if !strings.HasPrefix(audit.Orientation, "portrait") {
					issues = append(issues, fmt.Sprintf("%s %s: orientation %q is not portrait", page.Name, vp.ID, audit.Orientation))
				}
			}

			captures := []struct {
				kind string
				png  []byte
				dst  *[]ScreenshotResult
			}{
				{screenshotViewport, viewportPNG, &viewportResults},
				{screenshotFullPage, fullPagePNG, &fullPageResults},
			}
			for _, capture := range captures {
				filename := fmt.Sprintf("screen-%02d-%s.png", shotIndex, capture.kind)
				imagePath := filepath.Join(blindDir, filename)
				if len(capture.png) < 10_000 {
					issues = append(issues, fmt.Sprintf("%s %s %s: screenshot suspiciously small (%d bytes)", page.Name, vp.ID, capture.kind, len(capture.png)))
				}
				width, height, dimensionErr := pngDimensions(capture.png)
				if dimensionErr != nil {
					issues = append(issues, fmt.Sprintf("%s %s %s: decode dimensions: %v", page.Name, vp.ID, capture.kind, dimensionErr))
					continue
				}
				expectedWidth := int(math.Round(float64(vp.Width) * expectedScale))
				expectedHeight := int(math.Round(float64(vp.Height) * expectedScale))
				if capture.kind == screenshotViewport && (width != expectedWidth || height != expectedHeight) {
					issues = append(issues, fmt.Sprintf("%s %s: viewport capture is %dx%d, want %dx%d", page.Name, vp.ID, width, height, expectedWidth, expectedHeight))
				}
				if capture.kind == screenshotFullPage && width != expectedWidth {
					issues = append(issues, fmt.Sprintf("%s %s: full-page capture width is %d, want %d", page.Name, vp.ID, width, expectedWidth))
				}
				if capture.kind == screenshotFullPage {
					expectedFullHeight := int(math.Ceil(audit.DocumentHeight * expectedScale))
					if math.Abs(float64(height-expectedFullHeight)) > 3 {
						issues = append(issues, fmt.Sprintf("%s %s: full-page capture height is %d, want about %d", page.Name, vp.ID, height, expectedFullHeight))
					}
				}
				if writeErr := os.WriteFile(imagePath, capture.png, 0o644); writeErr != nil {
					issues = append(issues, fmt.Sprintf("%s %s %s: save screenshot: %v", page.Name, vp.ID, capture.kind, writeErr))
					continue
				}
				result := ScreenshotResult{
					Page: page.Name, Path: page.Path, Viewport: vp.ID, Scheme: vp.Scheme, Kind: capture.kind,
					ImagePath: imagePath, ImageWidth: width, ImageHeight: height,
					ViewportWidth: vp.Width, ViewportHeight: vp.Height,
					DeviceScaleFactor: audit.DeviceScaleFactor, TouchPoints: audit.TouchPoints,
					Orientation: audit.Orientation, UserAgent: audit.UserAgent, DocumentHeight: audit.DocumentHeight,
					HTTPStatus: status, OverflowX: overflow, Bytes: len(capture.png),
				}
				if capture.kind == screenshotViewport {
					result.BoundsViolations = audit.Violations
					result.ContrastViolations = audit.ContrastViolations
				}
				*capture.dst = append(*capture.dst, result)
			}
		}
	}
	// Judges see the unscrolled viewport evidence first. Full-page captures are
	// supplemental context and cannot visually bury the initial-screen state.
	return append(viewportResults, fullPageResults...), issues
}

func isMobileViewport(vp Viewport) bool { return vp.Width <= 480 }

func pngDimensions(data []byte) (int, int, error) {
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0, err
	}
	return config.Width, config.Height, nil
}

func httpStatus(ctx context.Context, url string) int {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return 0
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}
