// Package axetest runs the embedded axe-core engine against pages in a
// headless browser; [ScanSchemes] is the entry point.
package axetest

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/framework/axecov"
)

//go:embed testdata/axe.min.js
var axeMinJS string

// Schemes lists the color schemes every page is scanned under.
var Schemes = []string{"dark", "light"}

// Violation is one axe-core rule violation.
type Violation struct {
	ID          string         `json:"id"`
	Impact      string         `json:"impact"`
	Description string         `json:"description"`
	Help        string         `json:"help"`
	HelpURL     string         `json:"helpUrl"`
	Tags        []string       `json:"tags"`
	Nodes       []ViolatedNode `json:"nodes"`

	// Scheme is the forced color scheme, set by Scan.
	Scheme string `json:"-"`
}

// ViolatedNode is one element that tripped a rule.
type ViolatedNode struct {
	HTML           string   `json:"html"`
	Target         []string `json:"target"`
	FailureSummary string   `json:"failureSummary"`
}

// NewBrowser starts a headless browser that closes at test cleanup.
func NewBrowser(t *testing.T) context.Context {
	t.Helper()
	browserCtx, cancel, err := NewBrowserContext(context.Background())
	if err != nil {
		t.Fatalf("axetest: start browser: %v", err)
	}
	t.Cleanup(cancel)
	return browserCtx
}

// NewBrowserContext starts a headless browser, or errors; call cancel.
func NewBrowserContext(parent context.Context) (context.Context, context.CancelFunc, error) {
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.WSURLReadTimeout(90*time.Second),
		chromedp.WindowSize(1280, 800),
	)
	allocCtx, allocCancel := chromedp.NewExecAllocator(parent, opts...)
	browserCtx, browserCancel := chromedp.NewContext(allocCtx, chromedp.WithBrowserOption(chromedp.WithDialTimeout(90*time.Second)), chromedp.WithErrorf(filteredErrorf))
	cancel := func() { browserCancel(); allocCancel() }
	if err := chromedp.Run(browserCtx); err != nil {
		cancel()
		return nil, nil, fmt.Errorf("start headless browser: %w", err)
	}
	return browserCtx, cancel, nil
}

// NewTab opens a tab with a 30-second deadline; cancel closes it.
func NewTab(t *testing.T, browser context.Context) (context.Context, context.CancelFunc) {
	t.Helper()
	return NewTabContext(browser, 30*time.Second)
}

// NewTabContext is [NewTab] with a chosen deadline.
func NewTabContext(browser context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	tabCtx, tabCancel := chromedp.NewContext(browser)
	ctx, cancel := context.WithTimeout(tabCtx, timeout)
	return ctx, func() { cancel(); tabCancel() }
}

func filteredErrorf(format string, args ...any) {
	if strings.Contains(fmt.Sprintf(format, args...), "unhandled node event") {
		return
	}
	log.Printf("ERROR: "+format, args...)
}

const readyJS = `(() => {
	if (document.readyState !== 'complete') return 'document.readyState is ' + document.readyState;
	if (document.fonts && document.fonts.status !== 'loaded') return 'web fonts are still loading';
	const G = window.__gofastr;
	if (document.querySelector('script[src*="/__gofastr/runtime.js"]') && !G) return 'the GoFastr runtime has not booted';
	if (document.documentElement.getAttribute('aria-busy') === 'true') return 'a client navigation is in flight (<html aria-busy>)';
	const lm = (G && G.loadedModules) || {};
	for (const s of document.querySelectorAll('script[src*="/__gofastr/runtime/"]')) {
		const m = /\/__gofastr\/runtime\/([\w-]+)\.js$/.exec(new URL(s.src, location.href).pathname);
		if (m && !(Object.prototype.hasOwnProperty.call(lm, m[1]) && lm[m[1]])) return 'runtime module ' + m[1] + ' has not registered';
	}
	return '';
})()`

const readyPoll = 25 * time.Millisecond

var readyTimeout = 10 * time.Second

func pollSettled(ctx context.Context, js, what string) error {
	deadline := time.Now().Add(readyTimeout)
	hits := 0
	reason := "never evaluated"
	for {
		var got string
		if err := chromedp.Evaluate(js, &got).Do(ctx); err != nil {
			return fmt.Errorf("axetest: wait for %s: %w", what, err)
		}
		if got == "" {
			hits++
			if hits == 2 {
				return nil
			}
		} else {
			hits = 0
			reason = got
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("axetest: %s not reached within %s: %s", what, readyTimeout, reason)
		}
		if err := chromedp.Sleep(readyPoll).Do(ctx); err != nil {
			return fmt.Errorf("axetest: wait for %s: %w", what, err)
		}
	}
}

// WaitReady waits until the page, its fonts and every runtime module it
// requested have loaded, failing with the unmet condition after 10s.
func WaitReady() chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		return pollSettled(ctx, readyJS, "page ready")
	})
}

// Prepare freezes transitions and animations and forces scheme on the
// current document, returning once the scheme has taken effect.
func Prepare(scheme string) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		if err := chromedp.Evaluate(`(() => {
			const s = new CSSStyleSheet();
			s.replaceSync('*, *::before, *::after { transition: none !important; animation: none !important; }');
			document.adoptedStyleSheets = [...document.adoptedStyleSheets, s];
		})()`, nil).Do(ctx); err != nil {
			return err
		}
		if err := chromedp.Evaluate(fmt.Sprintf(
			`(() => {
				const mode = %q;
				if (window.__gofastr_colorScheme && typeof window.__gofastr_colorScheme.set === "function") {
					window.__gofastr_colorScheme.set(mode);
					return;
				}
				document.documentElement.setAttribute("data-color-scheme", mode);
				let meta = document.querySelector('meta[name="color-scheme"]');
				if (!meta) {
					meta = document.createElement("meta");
					meta.setAttribute("name", "color-scheme");
					document.head.appendChild(meta);
				}
				meta.setAttribute("content", mode);
			})()`, scheme), nil).Do(ctx); err != nil {
			return err
		}
		return pollSettled(ctx, schemeAppliedJS(scheme), scheme+" color scheme")
	})
}

func schemeAppliedJS(scheme string) string {
	return fmt.Sprintf(`(() => {
		const mode = %q;
		const root = document.documentElement;
		if (root.getAttribute('data-color-scheme') !== mode) return '<html data-color-scheme> is ' + root.getAttribute('data-color-scheme');
		const meta = document.querySelector('meta[name="color-scheme"]');
		if (!meta || meta.getAttribute('content') !== mode) return '<meta name="color-scheme"> is ' + (meta && meta.getAttribute('content'));
		const cs = getComputedStyle(document.body || root);
		const now = mode + '|' + cs.color + '|' + cs.backgroundColor;
		const prev = window.__axetestSchemeColors;
		window.__axetestSchemeColors = now;
		return prev === now ? '' : 'body colors still changing (' + now + ')';
	})()`, scheme)
}

// ScanSchemes runs [Prepare] and [Scan] on the current document for each
// scheme; an empty list is an error.
func ScanSchemes(ctx context.Context, schemes []string, ruleAllowlist map[string]string, opts ...ScanOption) ([]Violation, error) {
	if len(schemes) == 0 {
		return nil, fmt.Errorf("axetest: ScanSchemes got no schemes; an empty list would scan nothing and pass")
	}
	var kept []Violation
	for _, scheme := range schemes {
		if err := chromedp.Run(ctx, Prepare(scheme)); err != nil {
			return kept, fmt.Errorf("prepare %s scheme: %w", scheme, err)
		}
		vs, err := Scan(ctx, scheme, ruleAllowlist, opts...)
		if err != nil {
			return kept, err
		}
		kept = append(kept, vs...)
	}
	return kept, nil
}

// ScanOption changes the rules [Scan] passes to axe.run().
type ScanOption func(*scanConfig)

type scanConfig struct {
	disabledRules []string
	enabledRules  []string
}

// WithDisabledRules turns the given axe rules off.
func WithDisabledRules(rules ...string) ScanOption {
	return func(c *scanConfig) { c.disabledRules = append(c.disabledRules, rules...) }
}

// WithEnabledRules turns on axe rules that are off by default.
func WithEnabledRules(rules ...string) ScanOption {
	return func(c *scanConfig) { c.enabledRules = append(c.enabledRules, rules...) }
}

// Scan runs axe on the current DOM and returns the violations whose rule
// ruleAllowlist does not name. It errors on a blank page or a crashed rule.
func Scan(ctx context.Context, scheme string, ruleAllowlist map[string]string, opts ...ScanOption) ([]Violation, error) {
	var cfg scanConfig
	for _, o := range opts {
		o(&cfg)
	}

	var elementCount int
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelectorAll('body *').length`, &elementCount),
	); err != nil {
		return nil, fmt.Errorf("axe pre-scan (%s scheme): population check: %w", scheme, err)
	}
	if elementCount < minBodyElements {
		return nil, fmt.Errorf("axe pre-scan (%s scheme): page rendered only %d elements under <body> (need ≥%d): the page is blank or not rendered; refusing a vacuous pass", scheme, elementCount, minBodyElements)
	}

	var injected bool
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`window.__axetestEngine !== undefined && window.__axetestEngine === window.axe && typeof window.axe.run === 'function'`, &injected),
	); err != nil {
		return nil, fmt.Errorf("axe scan (%s scheme): probe engine: %w", scheme, err)
	}
	if !injected {
		if err := chromedp.Run(ctx, chromedp.Evaluate(";"+axeMinJS+";window.__axetestEngine = window.axe;void 0", nil)); err != nil {
			return nil, fmt.Errorf("axe scan (%s scheme): inject engine: %w", scheme, err)
		}
	}
	rulesJS := axeRulesJS(cfg)
	var raw string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`(async () => {
			const r = await axe.run(document, {
				resultTypes: ['violations'],
				rules: `+rulesJS+`
			});
			const crashed = r.incomplete.filter(x => x.error).map(x => ({
				id: x.id,
				error: String((x.error && x.error.message) || x.error)
			}));
			return JSON.stringify({violations: r.violations, crashed});
		})()`, &raw, evalAwaitPromise),
	); err != nil {
		return nil, fmt.Errorf("axe scan (%s scheme): %w", scheme, err)
	}
	var res struct {
		Violations []Violation `json:"violations"`
		Crashed    []struct {
			ID    string `json:"id"`
			Error string `json:"error"`
		} `json:"crashed"`
	}
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		return nil, fmt.Errorf("parse axe violations (%s scheme): %w\nraw=%s", scheme, err, raw)
	}
	for _, c := range res.Crashed {
		if _, ok := ruleAllowlist[c.ID]; ok {
			continue
		}
		return nil, fmt.Errorf("axe scan (%s scheme): rule %s crashed and checked nothing: %s", scheme, c.ID, c.Error)
	}
	var kept []Violation
	for _, v := range res.Violations {
		if _, ok := ruleAllowlist[v.ID]; ok {
			continue
		}
		v.Scheme = scheme
		kept = append(kept, v)
	}
	recordCoverage(ctx, scheme)
	return kept, nil
}

func recordCoverage(ctx context.Context, scheme string) {
	if os.Getenv("GOFASTR_AXE_COVERAGE") == "0" {
		return
	}
	var loc string
	if err := chromedp.Run(ctx, chromedp.Location(&loc)); err != nil {
		log.Printf("axetest: coverage: read page location: %v", err)
		return
	}
	if err := axecov.Record(axecov.DefaultDir(), loc, scheme); err != nil {
		log.Printf("axetest: coverage: %v", err)
	}
}

const minBodyElements = 5

func axeRulesJS(c scanConfig) string {
	if len(c.enabledRules) == 0 && len(c.disabledRules) == 0 {
		return "{}"
	}
	var b strings.Builder
	b.WriteString("{ ")
	first := true
	emit := func(rule, state string) {
		if !first {
			b.WriteString(", ")
		}
		first = false
		fmt.Fprintf(&b, "%q: { enabled: %s }", rule, state)
	}
	for _, r := range c.enabledRules {
		emit(r, "true")
	}
	for _, r := range c.disabledRules {
		emit(r, "false")
	}
	b.WriteString(" }")
	return b.String()
}

func evalAwaitPromise(p *runtime.EvaluateParams) *runtime.EvaluateParams {
	p.AwaitPromise = true
	return p
}
