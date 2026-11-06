package runtime

import (
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

func TestFallbackToastUsesRoleOnly(test *testing.T) {
	for _, blocked := range []bool{true, false} {
		name := "missing-stack"
		if blocked {
			name = "missing-module"
		}
		test.Run(name, func(test *testing.T) {
			feedback, ok := Module("headless-feedback")
			if !ok {
				test.Fatal("headless-feedback module is not registered")
			}
			var moduleRequests atomic.Int32
			base := startPollServer(test, `<!doctype html><html><head>
				<script type="application/json" id="gofastr-routes">[{"path":"/"}]</script>
				</head><body><main tabindex="-1">
				<button id="notify" data-cui-rpc="/notify" data-cui-rpc-method="POST">Notify</button>
				</main><script src="/__gofastr/runtime.js"></script></body></html>`, map[string]http.HandlerFunc{
				"/__gofastr/runtime/headless-feedback.js": func(writer http.ResponseWriter, request *http.Request) {
					moduleRequests.Add(1)
					if blocked {
						http.NotFound(writer, request)
						return
					}
					writer.Header().Set("Content-Type", "application/javascript")
					fmt.Fprint(writer, feedback)
				},
				"/notify": func(writer http.ResponseWriter, request *http.Request) {
					writer.Header().Set("Content-Type", "application/json")
					writer.Header().Set("X-Gofastr-Toast", `[{"title":"Default"},{"title":"Info","variant":"info"},{"title":"Success","variant":"success"},{"title":"Warning","variant":"warning"},{"title":"Danger","variant":"danger"},{"title":"Custom","variant":"brand"}]`)
					fmt.Fprint(writer, `{"success":true}`)
				},
			})
			ctx := chromedptest.Context(test)
			var rows []struct {
				Title   string `json:"title"`
				Role    string `json:"role"`
				HasLive bool   `json:"hasLive"`
			}
			var loaded bool
			if err := chromedp.Run(ctx,
				chromedp.Navigate(base),
				chromedp.Click(`#notify`, chromedp.ByID),
				chromedp.Poll(`document.querySelector('[data-cui-toast-fallback]')?.children.length === 6`, nil),
				chromedp.Evaluate(`Array.from(document.querySelector('[data-cui-toast-fallback]').children, row => ({
					title: row.querySelector('strong').textContent,
					role: row.getAttribute('role'), hasLive: row.hasAttribute('aria-live')
				}))`, &rows),
				chromedp.Evaluate(`!!window.__gofastr.loadedModules['headless-feedback']`, &loaded),
			); err != nil {
				test.Fatal(err)
			}
			if moduleRequests.Load() == 0 || loaded == blocked {
				test.Fatalf("feedback path not exercised: requests=%d loaded=%t blocked=%t", moduleRequests.Load(), loaded, blocked)
			}
			for _, row := range rows {
				wantRole := "status"
				if row.Title == "Warning" || row.Title == "Danger" {
					wantRole = "alert"
				}
				if row.Role != wantRole || row.HasLive {
					test.Errorf("%s fallback: role=%q aria-live present=%t, want role=%q only", row.Title, row.Role, row.HasLive, wantRole)
				}
			}
		})
	}
}

func TestSignalRolesAvoidDuplicateLive(test *testing.T) {
	base := startPollServer(test, `<!doctype html><html><head>
		<script type="application/json" id="gofastr-routes">[{"path":"/"},{"path":"/next"}]</script>
		</head><body><main tabindex="-1">
		<span id="plain" data-cui-signal="plain">0</span>
		<span id="status" role="status" data-cui-signal="status">0</span>
		<span id="alert" role="alert" data-cui-signal="alert">0</span>
		<span id="group" role="group" data-cui-signal="group">0</span>
		<span id="off" role="group" aria-live="off" data-cui-signal="off">0</span>
		<span id="override" role="status" aria-live="off" data-cui-signal="override">0</span>
		<span id="attr" data-cui-signal="attr" data-cui-signal-mode="attr" data-cui-signal-attr="title">0</span>
		<span id="html" data-cui-signal="html" data-cui-signal-mode="html">0</span>
		</main><script src="/__gofastr/runtime.js"></script></body></html>`, nil)
	ctx := chromedptest.Context(test)
	if err := chromedp.Run(ctx, chromedp.Navigate(base)); err != nil {
		test.Fatal(err)
	}
	for _, path := range []string{"/", "/next"} {
		if path != "/" {
			if err := chromedp.Run(ctx,
				chromedp.Evaluate(`window.__signalBeforeNav = document.getElementById('plain')`, nil),
				chromedp.Evaluate(`window.__gofastr.navigate('/next')`, nil),
				chromedp.Poll(`location.pathname === '/next' && document.getElementById('plain') !== window.__signalBeforeNav`, nil),
			); err != nil {
				test.Fatal(err)
			}
		}
		var nodes []struct {
			ID     string  `json:"id"`
			Role   string  `json:"role"`
			Live   *string `json:"live"`
			Atomic *string `json:"atomic"`
		}
		if err := chromedp.Run(ctx,
			chromedp.Poll(`document.getElementById('plain')?.getAttribute('aria-atomic') === 'true'`, nil),
			chromedp.Evaluate(`Array.from(document.querySelectorAll('[data-cui-signal]'), node => ({
				id: node.id, role: node.getAttribute('role') || '',
				live: node.getAttribute('aria-live'), atomic: node.getAttribute('aria-atomic')
			}))`, &nodes),
		); err != nil {
			test.Fatal(err)
		}
		if len(nodes) != 8 {
			test.Fatalf("%s: got %d signal nodes, want 8", path, len(nodes))
		}
		for _, node := range nodes {
			wantRole, wantLive := "status", ""
			switch node.ID {
			case "alert":
				wantRole = "alert"
			case "group":
				wantRole, wantLive = "group", "polite"
			case "off":
				wantRole, wantLive = "group", "off"
			case "override":
				wantLive = "off"
			case "attr", "html":
				wantRole = ""
			}
			if node.Role != wantRole || (node.Live != nil) != (wantLive != "") || node.Live != nil && *node.Live != wantLive {
				live := "<absent>"
				if node.Live != nil {
					live = *node.Live
				}
				test.Errorf("%s %s: role=%q aria-live=%q, want role=%q aria-live=%q", path, node.ID, node.Role, live, wantRole, wantLive)
			}
			if node.ID == "attr" || node.ID == "html" {
				if node.Atomic != nil {
					test.Errorf("%s %s: non-text binding gained aria-atomic", path, node.ID)
				}
			} else if node.Atomic == nil || *node.Atomic != "true" {
				test.Errorf("%s %s: text binding lost aria-atomic", path, node.ID)
			}
		}
	}
}
