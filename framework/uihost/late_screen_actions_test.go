package uihost

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/router"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
)

// A screen registered after Mount (a plugin's Init runs at Start) had no
// compiled actions, so every data-action click answered 404 (U55).
// App.Start's ValidateBoot compiles them.
func TestLateScreenActionsCompileAtBoot(t *testing.T) {
	ran := false
	late := &actionTestComp{html: "<p>late</p>", actions: func() {
		component.On("save", func(*component.ComponentContext) { ran = true })
	}}
	a := app.NewApp("late-actions")
	a.RegisterScreen(app.NewScreen("/", &testHomeComp{}).WithTitle("Home"), nil)
	ds := New(a)
	rtr := router.New()
	ds.Mount(rtr)
	a.RegisterScreen(app.NewScreen("/late", late).WithTitle("Late"), nil)
	ds.ValidateBoot()

	sess := ds.CreateSession()
	req := httptest.NewRequest(http.MethodPost, "/__gofastr/action",
		strings.NewReader(`{"action":"save","params":{},"componentId":"late"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: sessionCookieSecureName, Value: sess.Token})
	w := httptest.NewRecorder()
	rtr.ServeHTTP(w, req)
	if !ran {
		t.Fatalf("the late screen's action did not run: %d %s", w.Code, w.Body.String())
	}
}

// The embed refusal (a surface whose screen reaches G.serverAction) and the
// action-id collision refusal cover a late screen too.
func TestLateScreenRefusalsAtBoot(t *testing.T) {
	cases := map[string]struct {
		want  string
		build func(*app.App) *UIHost
	}{
		"embed server action": {"G.serverAction", func(a *app.App) *UIHost {
			eh, err := fembed.New(fembed.Config{
				Surfaces: []fembed.Surface{{
					Name:    "reports",
					Screen:  routeOnlyEmbedScreen("/reports"),
					Origins: []string{embedTestOrigin},
				}},
				BurnStore: fembed.NewMemoryBurnStore(),
			})
			if err != nil {
				t.Fatalf("embed.New: %v", err)
			}
			eh.SetKeys([]byte("nonce-key-nonce-key-nonce-key-32"), []byte("grant-key-grant-key-grant-key-32"))
			ds := New(a, WithEmbed(eh))
			ds.Mount(router.New())
			a.RegisterScreen(app.NewScreen("/reports", &serverActionScreenComp{}), nil)
			return ds
		}},
		"action id collision": {"both derive server-action id", func(a *app.App) *UIHost {
			a.RegisterScreen(app.NewScreen("/admin/users", &plainActionScreenComp{}), nil)
			ds := New(a)
			ds.Mount(router.New())
			a.RegisterScreen(app.NewScreen("/admin-users", &serverActionScreenComp{}), nil)
			return ds
		}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			a := app.NewApp("late-refusals")
			a.RegisterScreen(app.NewScreen("/", &testHomeComp{}).WithTitle("Home"), nil)
			ds := c.build(a)
			defer func() {
				if got := fmt.Sprint(recover()); !strings.Contains(got, c.want) {
					t.Fatalf("ValidateBoot panic = %q, want it to name %q", got, c.want)
				}
			}()
			ds.ValidateBoot()
		})
	}
}
