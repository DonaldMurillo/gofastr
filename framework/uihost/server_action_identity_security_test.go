package uihost

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/handler"
)

// CHAIN1-R1: the server-action invocation seam drops the HTTP request
// context, so the documented per-handler authorization check is
// unimplementable.
//
// uihost.go:2432-2434 tells every handler:
//
//	this check does not make server actions privileged, and a server action
//	is NOT an authorization boundary: a handler that mutates anything must
//	check authorization itself.
//
// but uihost.go:2443 builds the handler context as
// component.NewComponentContext(actionName, "", body.Params) — r.Context(),
// where the auth middlewares install the caller via handler.SetUser
// (battery/auth/middleware.go, battery/auth/jwt.go, apitoken middleware),
// never crosses the seam, and core-ui/component/context.go:7-16 hands the
// handler only EventName/TargetID/Params.
//
// Property: a request that carries an authenticated caller on r.Context()
// (the exact shape production auth middleware produces) reaches a
// registered action handler, and the value the handler receives must let
// it observe that caller. Pinned by a test-registered action that
// inspects the *component.ComponentContext it is handed and reports what
// it can see.
type actionCallerUser struct{ id string }

func TestActionHandlerObservesCaller(t *testing.T) {
	var report struct {
		handlerRan bool
		eventName  string
		targetID   string
		params     map[string]string
		isCtx      bool
		userOK     bool
		userID     string
	}
	ic := &actionTestComp{
		html: "<p>identity</p>",
		actions: func() {
			component.On("mutate", func(cc *component.ComponentContext) {
				report.handlerRan = true
				report.eventName = cc.EventName
				report.targetID = cc.TargetID
				report.params = cc.Params
				// cc is the ONLY value the handler receives. Today it is a
				// plain struct with five fields and no request context; a fix
				// that threads r.Context() to the handler must make the
				// caller observable through this value.
				if cctx, ok := any(cc).(context.Context); ok {
					report.isCtx = true
					if u, found := handler.GetUser(cctx); found {
						report.userOK = true
						if cu, ok := u.(*actionCallerUser); ok {
							report.userID = cu.id
						}
					}
				}
			})
		},
	}

	a := app.NewApp("action-identity")
	a.RegisterScreen(app.NewScreen("/", &testHomeComp{}).WithTitle("Home"), nil)
	ds := New(a)
	ds.CompileActions("test-comp", ic)

	// Anonymous session mint: POST /__gofastr/session with no Origin and no
	// grant header — the shape any non-browser caller takes
	// (rejectCrossOrigin passes when Origin is absent, uihost.go:2276-2279).
	mintRec := httptest.NewRecorder()
	ds.ServeHTTP(mintRec, httptest.NewRequest(http.MethodPost, "/__gofastr/session", nil))
	if mintRec.Code != http.StatusOK {
		t.Fatalf("session mint returned %d: cannot set up the authenticated-request premise", mintRec.Code)
	}
	cookies := mintRec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("session mint set no cookie")
	}

	// The action request carries an authenticated caller exactly the way
	// battery/auth middlewares do: handler.SetUser on r.Context().
	body := `{"action":"mutate","params":{"id":"42"},"componentId":"test-comp"}`
	req := httptest.NewRequest(http.MethodPost, "/__gofastr/action", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	req = req.WithContext(handler.SetUser(req.Context(), &actionCallerUser{id: "user-77"}))

	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("action POST returned %d body=%q: handler must be reached for this property", rec.Code, rec.Body.String())
	}
	if !report.handlerRan {
		t.Fatal("registered action handler did not run")
	}

	// RED pin: the handler-observable context must carry the caller.
	if !report.isCtx || !report.userOK || report.userID != "user-77" {
		t.Errorf("SECURITY: [action-identity] server action handler cannot see the authenticated caller. "+
			"The request carried user %q on r.Context() (handler.SetUser, the battery/auth middleware shape), "+
			"the handler ran, but everything it was handed was: EventName=%q TargetID=%q Params=%v. "+
			"*component.ComponentContext carries no context.Context, so handler.GetUser has nothing to read: "+
			"uihost.go:2443 builds the handler context with component.NewComponentContext(actionName, %q, body.Params) "+
			"and drops r.Context(), while uihost.go:2432-2434 demands 'a handler that mutates anything must check "+
			"authorization itself' — that check is unimplementable through this API",
			"user-77", report.eventName, report.targetID, report.params, "")
	}
}
