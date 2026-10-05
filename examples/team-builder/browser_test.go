package main

// The chromedp suite drives the team builder in a real browser: the
// form saves into IndexedDB, the list renders the saved team, a second
// tab sees every change, the edit and release controls work, the
// team cap holds, and a reload keeps everything. Set
// TEAM_BUILDER_SHOTS=<dir> to also write full-page screenshots (light
// and dark, desktop and phone) for reading the pixels.

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/cdproto/emulation"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

func startTeamServer(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(buildApp().Router())
	t.Cleanup(srv.Close)
	return srv.URL
}

// addMember fills and submits the form, then waits for the count.
func addMember(nickname, species, level, wantCount string) chromedp.Tasks {
	return chromedp.Tasks{
		chromedp.SetValue(`#member-nickname`, nickname, chromedp.ByQuery),
		chromedp.SetValue(`#member-species`, species, chromedp.ByQuery),
		chromedp.SetValue(`#member-level`, level, chromedp.ByQuery),
		chromedp.Click(`[data-fui-local-form] button[type=submit]`, chromedp.ByQuery),
		waitText(`[data-fui-local-count]`, wantCount),
	}
}

func waitText(sel, want string) chromedp.Action {
	return chromedp.Poll(`(document.querySelector(`+jsString(sel)+`) || {}).textContent === `+jsString(want),
		nil, chromedp.WithPollingInterval(25*time.Millisecond), chromedp.WithPollingTimeout(10*time.Second))
}

// front brings a tab to the foreground before driving it: a background
// tab stops running requestAnimationFrame, which chromedp.Poll waits on.
func front(ctx context.Context, t *testing.T) context.Context {
	t.Helper()
	c := chromedp.FromContext(ctx)
	if c == nil || c.Target == nil {
		return ctx
	}
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(cctx context.Context) error {
		return target.ActivateTarget(c.Target.TargetID).Do(cctx)
	})); err != nil {
		t.Fatalf("activate tab: %v", err)
	}
	return ctx
}

// waitItems waits until the list shows n member cards.
func waitItems(n int) chromedp.Action {
	return chromedp.Poll(`document.querySelectorAll('[data-fui-local-item][data-fui-local-key]').length === `+strconv.Itoa(n),
		nil, chromedp.WithPollingInterval(25*time.Millisecond), chromedp.WithPollingTimeout(10*time.Second))
}

func jsString(s string) string { return "'" + strings.ReplaceAll(s, "'", `\'`) + "'" }

// names reads the rendered team, in list order.
func names(ctx context.Context, t *testing.T) []string {
	t.Helper()
	var out []string
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`Array.from(document.querySelectorAll('[data-fui-local-item] [data-fui-local-text="nickname"]')).map((n) => n.textContent)`,
		&out)); err != nil {
		t.Fatalf("read team: %v", err)
	}
	return out
}

func shot(ctx context.Context, t *testing.T, name string) {
	t.Helper()
	dir := os.Getenv("TEAM_BUILDER_SHOTS")
	if dir == "" {
		return
	}
	var buf []byte
	if err := chromedp.Run(ctx, chromedp.FullScreenshot(&buf, 90)); err != nil {
		t.Fatalf("screenshot %s: %v", name, err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".png"), buf, 0o600); err != nil {
		t.Fatalf("write screenshot: %v", err)
	}
}

func TestTeamBuilderInBrowser(t *testing.T) {
	base := startTeamServer(t)
	tab := chromedptest.Context(t, chromedptest.Timeout(120*time.Second), chromedptest.WindowSize(1280, 900))

	if err := chromedp.Run(tab,
		chromedp.Navigate(base+"/"),
		chromedp.WaitVisible(`#member-nickname`, chromedp.ByQuery),
		waitText(`[data-fui-local-count]`, "0"),
		chromedp.WaitVisible(`[data-fui-local-item="empty"]`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("first load: %v", err)
	}
	shot(tab, t, "1-empty")

	if err := chromedp.Run(tab,
		addMember("Sparky", "Pikachu", "25", "1"),
		addMember("Blaze", "Charmander", "40", "2"),
		addMember("Snooze", "Snorlax", "12", "3"),
		waitItems(3),
	); err != nil {
		t.Fatalf("add members: %v", err)
	}
	if got := strings.Join(names(tab, t), ","); got != "Blaze,Sparky,Snooze" {
		t.Fatalf("team = %q, want ordered by level, highest first", got)
	}
	var formValue string
	if err := chromedp.Run(tab, chromedp.Value(`#member-nickname`, &formValue, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	if formValue != "" {
		t.Fatalf("the form kept %q after a save; want it reset", formValue)
	}
	shot(tab, t, "2-team")

	// A second tab sees the team and every later change.
	tab2, cancel := chromedp.NewContext(tab)
	defer cancel()
	if err := chromedp.Run(tab2,
		chromedp.Navigate(base+"/"),
		waitText(`[data-fui-local-count]`, "3"),
	); err != nil {
		t.Fatalf("second tab: %v", err)
	}
	if err := chromedp.Run(front(tab2, t), addMember("Shelly", "Squirtle", "30", "4")); err != nil {
		t.Fatalf("add in second tab: %v", err)
	}
	if err := chromedp.Run(front(tab, t), waitText(`[data-fui-local-count]`, "4"), waitItems(4)); err != nil {
		t.Fatalf("first tab never saw the second tab's save: %v", err)
	}
	if got := strings.Join(names(tab, t), ","); got != "Blaze,Shelly,Sparky,Snooze" {
		t.Fatalf("first tab team = %q after the other tab's save", got)
	}

	// Edit: Blaze's card (first, highest level) loads into the form;
	// saving updates it in place.
	steps := []struct {
		name string
		do   chromedp.Action
	}{
		{"click edit", chromedp.Click(`[data-fui-local-item][data-fui-local-key] [data-fui-local-edit] button`, chromedp.ByQuery)}, // Blaze, first card
		{"form filled", chromedp.Poll(`document.querySelector('#member-nickname').value === 'Blaze'`, nil, chromedp.WithPollingInterval(25*time.Millisecond), chromedp.WithPollingTimeout(5*time.Second))},
		{"set level", chromedp.SetValue(`#member-level`, "8", chromedp.ByQuery)},
		{"submit", chromedp.Click(`[data-fui-local-form] button[type=submit]`, chromedp.ByQuery)},
		{"moved last", chromedp.Poll(`document.querySelector('[data-fui-local-item]:last-child [data-fui-local-text="nickname"]').textContent === 'Blaze'`,
			nil, chromedp.WithPollingInterval(25*time.Millisecond), chromedp.WithPollingTimeout(5*time.Second))},
	}
	for _, st := range steps {
		if err := chromedp.Run(tab, st.do); err != nil {
			var dbg string
			_ = chromedp.Run(tab, chromedp.Evaluate(`document.querySelector('#member-nickname').value + ' | ' + Array.from(document.querySelectorAll('[data-fui-local-item] [data-fui-local-text="nickname"]')).map((n) => n.textContent).join(',')`, &dbg))
			t.Fatalf("edit, %s: %v (form/team: %s)", st.name, err, dbg)
		}
	}
	if got := strings.Join(names(tab, t), ","); got != "Shelly,Sparky,Snooze,Blaze" {
		t.Fatalf("after editing Blaze to level 8, team = %q", got)
	}
	var count string
	if err := chromedp.Run(tab, chromedp.Text(`[data-fui-local-count]`, &count, chromedp.ByQuery)); err != nil || count != "4" {
		t.Fatalf("an edit changed the count to %q (%v); it must update in place", count, err)
	}

	// Validation: a level the declaration refuses is placed beside
	// the field, and nothing is saved. The browser's own check is
	// bypassed (novalidate) to reach the behaviour's.
	if err := chromedp.Run(tab,
		chromedp.Evaluate(`document.querySelector('[data-fui-local-form] form').noValidate = true`, nil),
		chromedp.SetValue(`#member-nickname`, "Overflow", chromedp.ByQuery),
		chromedp.SetValue(`#member-species`, "Eevee", chromedp.ByQuery),
		chromedp.SetValue(`#member-level`, "500", chromedp.ByQuery),
		chromedp.Click(`[data-fui-local-form] button[type=submit]`, chromedp.ByQuery),
		chromedp.Poll(`!!document.querySelector('#member-level[aria-invalid="true"]')`, nil, chromedp.WithPollingInterval(25*time.Millisecond), chromedp.WithPollingTimeout(5*time.Second)),
	); err != nil {
		t.Fatalf("validation: %v", err)
	}
	var msg string
	if err := chromedp.Run(tab, chromedp.Evaluate(
		`document.querySelector('[data-hui-field-error="live"], [data-hui-field-error="filled"]').textContent`, &msg)); err != nil {
		t.Fatal(err)
	}
	if msg != "Must be at most 100." {
		t.Fatalf("level error = %q", msg)
	}
	shot(tab, t, "3-invalid")
	if err := chromedp.Run(tab,
		chromedp.Evaluate(`document.querySelector('[data-fui-local-form] form').noValidate = false`, nil),
		chromedp.Click(`[data-fui-local-form] button[type=reset]`, chromedp.ByQuery),
	); err != nil {
		t.Fatal(err)
	}

	// The cap: two more fill the team; a seventh is refused.
	if err := chromedp.Run(tab,
		addMember("Puff", "Jigglypuff", "20", "5"),
		addMember("Boo", "Gengar", "50", "6"),
		chromedp.SetValue(`#member-nickname`, "Extra", chromedp.ByQuery),
		chromedp.SetValue(`#member-species`, "Eevee", chromedp.ByQuery),
		chromedp.SetValue(`#member-level`, "3", chromedp.ByQuery),
		chromedp.Click(`[data-fui-local-form] button[type=submit]`, chromedp.ByQuery),
		chromedp.Poll(`document.body.textContent.includes('Your team is full: release a Pokémon first (6 at most).')`,
			nil, chromedp.WithPollingInterval(25*time.Millisecond), chromedp.WithPollingTimeout(5*time.Second)),
	); err != nil {
		t.Fatalf("cap: %v", err)
	}
	if got := len(names(tab, t)); got != 6 {
		t.Fatalf("team holds %d after a refused seventh, want 6", got)
	}
	shot(tab, t, "4-full")

	// Release from the second tab; the first tab follows.
	if err := chromedp.Run(front(tab2, t),
		waitText(`[data-fui-local-count]`, "6"),
		chromedp.Click(`[data-fui-local-item] [data-fui-local-delete] button`, chromedp.ByQuery),
		waitText(`[data-fui-local-count]`, "5"),
	); err != nil {
		t.Fatalf("release: %v", err)
	}
	if err := chromedp.Run(front(tab, t), waitText(`[data-fui-local-count]`, "5")); err != nil {
		t.Fatalf("first tab never saw the release: %v", err)
	}

	// A reload keeps the team: it lives in IndexedDB, not the page.
	if err := chromedp.Run(tab,
		chromedp.Reload(),
		waitText(`[data-fui-local-count]`, "5"),
		waitItems(5),
	); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := len(names(tab, t)); got != 5 {
		t.Fatalf("after reload the team shows %d, want 5", got)
	}
	shot(tab, t, "5-after-reload")

	if os.Getenv("TEAM_BUILDER_SHOTS") != "" {
		if err := chromedp.Run(tab,
			emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{{Name: "prefers-color-scheme", Value: "dark"}}),
			chromedp.Reload(),
			waitText(`[data-fui-local-count]`, "5"),
			waitItems(5),
		); err != nil {
			t.Fatal(err)
		}
		shot(tab, t, "6-dark")
		if err := chromedp.Run(tab,
			chromedp.EmulateViewport(390, 844, chromedp.EmulateMobile),
			chromedp.Reload(),
			waitText(`[data-fui-local-count]`, "5"),
			waitItems(5),
		); err != nil {
			t.Fatal(err)
		}
		shot(tab, t, "7-phone-dark")
		if err := chromedp.Run(tab,
			emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{{Name: "prefers-color-scheme", Value: "light"}}),
			chromedp.Reload(),
			waitText(`[data-fui-local-count]`, "5"),
			waitItems(5),
		); err != nil {
			t.Fatal(err)
		}
		shot(tab, t, "8-phone-light")
	}
}

// Records are untrusted on the way back out: anyone can edit
// IndexedDB from devtools, and any script on the origin can write it.
// A stored nickname that is markup renders as text, an object where a
// scalar belongs renders empty, and a form control the declaration
// does not name (an injected "id", a "__proto__") never reaches the
// record.
func TestTeamBuilderRecordsAreText(t *testing.T) {
	base := startTeamServer(t)
	tab := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	hostile := `<img src=x onerror="window.__pwned=1">`
	if err := chromedp.Run(tab,
		chromedp.Navigate(base+"/"),
		waitText(`[data-fui-local-count]`, "0"),
		chromedp.Evaluate(`(async () => {
			const db = await __gofastr.localdb.open('team-builder');
			await db.put('members', { nickname: `+jsString(hostile)+`, species: { evil: true }, level: 99,
				created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z' });
		})()`, nil, func(p *cdpruntime.EvaluateParams) *cdpruntime.EvaluateParams { return p.WithAwaitPromise(true) }),
		waitItems(1),
	); err != nil {
		t.Fatalf("plant: %v", err)
	}
	var probe struct {
		Imgs     int    `json:"imgs"`
		Nickname string `json:"nickname"`
		Species  string `json:"species"`
		Pwned    bool   `json:"pwned"`
	}
	if err := chromedp.Run(tab, chromedp.Evaluate(`({
		imgs: document.querySelectorAll('[data-fui-local-item] img').length,
		nickname: document.querySelector('[data-fui-local-item] [data-fui-local-text="nickname"]').textContent,
		species: document.querySelector('[data-fui-local-item] [data-fui-local-text="species"]').textContent,
		pwned: window.__pwned === 1,
	})`, &probe)); err != nil {
		t.Fatal(err)
	}
	if probe.Imgs != 0 || probe.Pwned || probe.Nickname != hostile {
		t.Fatalf("a stored nickname became markup: %+v", probe)
	}
	if probe.Species != "" {
		t.Fatalf("an object in a text slot rendered %q, want empty", probe.Species)
	}

	// Inject controls the declaration does not name, then save.
	var rec map[string]any
	if err := chromedp.Run(tab,
		chromedp.Evaluate(`(() => {
			const form = document.querySelector('[data-fui-local-form] form');
			for (const [n, v] of [['id', 'chosen-by-page'], ['__proto__', 'x'], ['created_at', '1999']]) {
				const i = document.createElement('input');
				i.type = 'hidden'; i.name = n; i.value = v;
				form.appendChild(i);
			}
		})()`, nil),
		addMember("Plain", "Eevee", "3", "2"),
		chromedp.Evaluate(`(async () => {
			const db = await __gofastr.localdb.open('team-builder');
			const all = await db.list('members', { index: 'by_level' });
			const r = all.find((m) => m.nickname === 'Plain');
			return { id: r.id, created: r.created_at, keys: Object.keys(r).sort().join(','), polluted: ({}).x === 'x' || Object.getPrototypeOf(r) !== Object.prototype };
		})()`, &rec, func(p *cdpruntime.EvaluateParams) *cdpruntime.EvaluateParams { return p.WithAwaitPromise(true) }),
	); err != nil {
		t.Fatalf("save with injected controls: %v", err)
	}
	if rec["id"] == "chosen-by-page" || rec["created"] == "1999" || rec["polluted"] == true {
		t.Fatalf("an undeclared control reached the record: %v", rec)
	}
	if rec["keys"] != "created_at,id,level,nickname,species,updated_at" {
		t.Fatalf("record keys = %v, want only the declared fields and the built-ins", rec["keys"])
	}
}
