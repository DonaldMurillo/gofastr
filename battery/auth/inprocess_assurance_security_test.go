package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework"
)

//gofastr:test-reads /gofastr.settings.yml

func newInProcessAssuranceRig(t *testing.T) (*AuthManager, *TwoFAPlugin, *MemorySessionStore, User) {
	t.Helper()
	users := newUserStoreWithPassword()
	user := &BasicUser{ID: "42", Email: "local@example.com", Roles: []string{"user"}}
	entry := &storeEntry{user: user}
	users.users[user.Email] = entry
	users.byID[user.ID] = entry
	sessions := NewMemorySessionStore()
	manager := New(AuthConfig{
		JWTSecret:           "inprocess-assurance-test",
		UserStore:           users,
		SessionStore:        sessions,
		AllowInMemoryStores: true,
	})
	twofa := NewTwoFAPlugin(TwoFAConfig{Store: NewMemoryTwoFAStore()})
	manager.Use(NewCorePlugin().WithTwoFactorURL("/login/2fa"))
	manager.Use(twofa)
	if err := manager.Init(nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := manager.OnStop(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return manager, twofa, sessions, user
}

func inProcessOuterContext(t *testing.T, manager *AuthManager, tokens ...string) context.Context {
	t.Helper()
	return inProcessOuterContextFrom(t, t.Context(), manager, tokens...)
}

func inProcessOuterContextFrom(t *testing.T, ctx context.Context, manager *AuthManager, tokens ...string) context.Context {
	t.Helper()
	var captured context.Context
	outer := SessionMiddleware(manager)(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		captured = request.Context()
	}))
	request := httptest.NewRequest(http.MethodGet, "/outer", nil).WithContext(ctx)
	for _, token := range tokens {
		request.AddCookie(&http.Cookie{Name: manager.Config().SessionCookie, Value: token})
	}
	outer.ServeHTTP(httptest.NewRecorder(), request)
	if captured == nil || GetCurrentUser(captured) == nil {
		t.Fatal("outer SessionMiddleware did not authenticate the cookie")
	}
	return captured
}

func inProcessAssuranceApp(manager *AuthManager, twofa *TwoFAPlugin) *framework.App {
	application := framework.NewApp()
	protected := twofa.RequireTwoFA()(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}))
	application.Router().Get("/protected", SessionMiddleware(manager)(RequireAuth(manager.JWT())(protected)))
	application.Router().Get("/twofa-only", protected)
	application.Router().Get("/session-only", SessionMiddleware(manager)(RequireSession()(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}))))
	manager.RegisterRoutes(application.Router())
	return application
}

func TestInProcessTwoFAAssurance(t *testing.T) {
	for _, test := range []struct {
		name       string
		unenrolled bool
		verified   bool
		second     string
		status     int
	}{
		{"SteppedUp", false, true, "", http.StatusNoContent},
		{"Unenrolled", true, false, "", http.StatusNoContent},
		{"Unverified", false, false, "", http.StatusForbidden},
		{"UnverifiedThenVerified", false, false, "verified", http.StatusNoContent},
		{"VerifiedThenUnverified", false, true, "unverified", http.StatusNoContent},
		{"OtherUserVerified", false, false, "other", http.StatusForbidden},
		{"PendingVerified", false, false, "pending", http.StatusForbidden},
		{"ExpiredVerified", false, false, "expired", http.StatusForbidden},
		{"RevokedVerified", false, false, "revoked", http.StatusForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			manager, twofa, sessions, user := newInProcessAssuranceRig(t)
			if !test.unenrolled {
				if err := twofa.store.SetTwoFA(t.Context(), user.GetID(), &TwoFAState{Enabled: true}); err != nil {
					t.Fatal(err)
				}
			}
			session, err := sessions.Create(t.Context(), user.GetID(), time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			if test.verified {
				if err := sessions.MarkTwoFactorVerified(t.Context(), session.Token); err != nil {
					t.Fatal(err)
				}
			}
			tokens := []string{session.Token}
			if test.second != "" {
				secondUser := user
				if test.second == "other" {
					secondUser, err = manager.UserStore().CreateUser(t.Context(), "other@example.com", "", nil)
					if err != nil {
						t.Fatal(err)
					}
				}
				steppedUp, err := sessions.Create(t.Context(), secondUser.GetID(), time.Hour)
				if err != nil {
					t.Fatal(err)
				}
				if test.second != "unverified" {
					if err := sessions.MarkTwoFactorVerified(t.Context(), steppedUp.Token); err != nil {
						t.Fatal(err)
					}
				}
				switch test.second {
				case "pending":
					if err := sessions.MarkPendingTwoFactor(t.Context(), steppedUp.Token); err != nil {
						t.Fatal(err)
					}
				case "expired":
					sessions.mu.Lock()
					sessions.sessions[steppedUp.Token].ExpiresAt = time.Now().Add(-time.Hour)
					sessions.mu.Unlock()
				case "revoked":
					if err := sessions.Delete(t.Context(), steppedUp.Token); err != nil {
						t.Fatal(err)
					}
				}
				tokens = append(tokens, steppedUp.Token)
			}
			application := inProcessAssuranceApp(manager, twofa)
			outerContext := inProcessOuterContext(t, manager, tokens...)
			network := httptest.NewRequest(http.MethodGet, "/twofa-only", nil).WithContext(outerContext)
			for _, token := range tokens {
				network.AddCookie(&http.Cookie{Name: manager.Config().SessionCookie, Value: token})
			}
			for _, transport := range []string{"Network", "Local"} {
				t.Run(transport, func(t *testing.T) {
					request := network
					var target http.Handler = application.Router()
					if transport == "Local" {
						request = httptest.NewRequest(http.MethodGet, "/protected", nil).WithContext(outerContext)
						target = application.InProcessHandler()
					}
					recorder := httptest.NewRecorder()
					target.ServeHTTP(recorder, request)
					if recorder.Code != test.status {
						t.Fatalf("status = %d, want %d: %s", recorder.Code, test.status, recorder.Body.String())
					}
				})
			}
		})
	}
}

func TestInProcessTwoFAAssurancePrincipalBinding(t *testing.T) {
	manager, twofa, sessions, user := newInProcessAssuranceRig(t)
	session, err := sessions.Create(t.Context(), user.GetID(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	outerContext := inProcessOuterContext(t, manager, session.Token)
	application := inProcessAssuranceApp(manager, twofa)
	for _, test := range []struct {
		name string
		ctx  context.Context
	}{
		{"SwappedUser", handler.SetUser(outerContext, &BasicUser{ID: "other-user"})},
		{"ClearedUser", handler.SetUser(outerContext, nil)},
		{"NoAssurance", handler.SetUser(t.Context(), user)},
		{"NoAssuranceEmptyUserID", handler.SetUser(t.Context(), &BasicUser{})},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			application.InProcessHandler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/twofa-only", nil).WithContext(test.ctx))
			if recorder.Code != http.StatusUnauthorized || !strings.Contains(recorder.Body.String(), `"reason":"no_session"`) {
				t.Fatalf("status = %d, want 401 no session: %s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestInProcessTwoFAAssuranceManagerBinding(t *testing.T) {
	issuer, _, issuerSessions, issuerUser := newInProcessAssuranceRig(t)
	receiver, twofa, receiverSessions, receiverUser := newInProcessAssuranceRig(t)
	if issuerUser.GetID() != "42" || receiverUser.GetID() != issuerUser.GetID() {
		t.Fatal("auth realms must contain the same user id")
	}
	if err := twofa.store.SetTwoFA(t.Context(), receiverUser.GetID(), &TwoFAState{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	for _, manager := range []*AuthManager{issuer, receiver} {
		sessions := issuerSessions
		if manager == receiver {
			sessions = receiverSessions
		}
		session, err := sessions.Create(t.Context(), "42", time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if err := sessions.MarkTwoFactorVerified(t.Context(), session.Token); err != nil {
			t.Fatal(err)
		}
		ctx := handler.SetUser(inProcessOuterContext(t, manager, session.Token), receiverUser)
		status := http.StatusUnauthorized
		name := "OtherManager"
		if manager == receiver {
			status = http.StatusNoContent
			name = "OwnManager"
		}
		t.Run(name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			inProcessAssuranceApp(receiver, twofa).InProcessHandler().ServeHTTP(recorder,
				httptest.NewRequest(http.MethodGet, "/protected", nil).WithContext(ctx))
			if recorder.Code != status || status == http.StatusUnauthorized && !strings.Contains(recorder.Body.String(), `"reason":"no_session"`) {
				t.Fatalf("status = %d, want %d: %s", recorder.Code, status, recorder.Body.String())
			}
		})
	}
}

func TestInProcessTwoFAAssuranceTenantBinding(t *testing.T) {
	manager, twofa, sessions, user := newInProcessAssuranceRig(t)
	if err := twofa.store.SetTwoFA(t.Context(), user.GetID(), &TwoFAState{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	session, err := sessions.Create(t.Context(), user.GetID(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := sessions.MarkTwoFactorVerified(t.Context(), session.Token); err != nil {
		t.Fatal(err)
	}
	ctx := inProcessOuterContextFrom(t, handler.SetTenantID(t.Context(), "tenant-a"), manager, session.Token)
	for _, test := range []struct {
		tenant string
		status int
	}{
		{"tenant-a", http.StatusNoContent},
		{"tenant-b", http.StatusUnauthorized},
		{"", http.StatusUnauthorized},
	} {
		t.Run(test.tenant, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/protected", nil).WithContext(handler.SetTenantID(ctx, test.tenant))
			inProcessAssuranceApp(manager, twofa).InProcessHandler().ServeHTTP(recorder, request)
			if recorder.Code != test.status {
				t.Fatalf("status = %d, want %d: %s", recorder.Code, test.status, recorder.Body.String())
			}
		})
	}
}

func TestInProcessTwoFAAssuranceRecheckCandidates(t *testing.T) {
	for _, test := range []struct {
		name         string
		change       string
		secondUserID string
		recheck      bool
		status       int
	}{
		{"LaterVerified", "", "42", true, http.StatusNoContent},
		{"FirstRevoked", "first-revoked", "42", true, http.StatusNoContent},
		{"AllRevoked", "all-revoked", "42", false, 0},
		{"LaterPending", "pending", "42", true, http.StatusForbidden},
		{"LaterExpired", "expired", "42", true, http.StatusForbidden},
		{"LaterRevoked", "second-revoked", "42", true, http.StatusForbidden},
		{"OtherUser", "", "other-user", true, http.StatusForbidden},
		{"CannotSwitchUser", "first-revoked", "other-user", false, 0},
		{"CannotSwitchTenant", "tenant", "42", false, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			manager, twofa, sessions, user := newInProcessAssuranceRig(t)
			if err := twofa.store.SetTwoFA(t.Context(), user.GetID(), &TwoFAState{Enabled: true}); err != nil {
				t.Fatal(err)
			}
			first, err := sessions.Create(t.Context(), user.GetID(), time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			second, err := sessions.Create(t.Context(), test.secondUserID, time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			if err := sessions.MarkTwoFactorVerified(t.Context(), second.Token); err != nil {
				t.Fatal(err)
			}
			users := manager.UserStore().(*userStoreWithPassword)
			users.byID["other-user"] = &storeEntry{user: &BasicUser{ID: "other-user"}}
			ctx := inProcessOuterContextFrom(t, handler.SetTenantID(t.Context(), "tenant-a"), manager, first.Token, second.Token)
			switch test.change {
			case "all-revoked", "first-revoked":
				if err := sessions.Delete(t.Context(), first.Token); err != nil {
					t.Fatal(err)
				}
			}
			switch test.change {
			case "all-revoked", "second-revoked":
				if err := sessions.Delete(t.Context(), second.Token); err != nil {
					t.Fatal(err)
				}
			case "pending":
				if err := sessions.MarkPendingTwoFactor(t.Context(), second.Token); err != nil {
					t.Fatal(err)
				}
			case "expired":
				sessions.mu.Lock()
				sessions.sessions[second.Token].ExpiresAt = time.Now().Add(-time.Hour)
				sessions.mu.Unlock()
			case "tenant":
				ctx = handler.SetTenantID(ctx, "tenant-b")
			}
			refreshed, ok := handler.RecheckPrincipal(ctx)
			if ok != test.recheck {
				t.Fatalf("RecheckPrincipal = %v, want %v", ok, test.recheck)
			}
			if !ok {
				return
			}
			for attempt := 0; attempt < 2; attempt++ {
				recorder := httptest.NewRecorder()
				inProcessAssuranceApp(manager, twofa).InProcessHandler().ServeHTTP(recorder,
					httptest.NewRequest(http.MethodGet, "/protected", nil).WithContext(refreshed))
				if recorder.Code != test.status {
					t.Fatalf("status = %d, want %d: %s", recorder.Code, test.status, recorder.Body.String())
				}
				refreshed, ok = handler.RecheckPrincipal(refreshed)
				if !ok {
					t.Fatal("refreshed principal lost its recheck")
				}
			}
		})
	}
}

func TestInProcessTwoFAAssuranceResolvesPrincipalOnce(t *testing.T) {
	manager, twofa, sessions, user := newInProcessAssuranceRig(t)
	if err := twofa.store.SetTwoFA(t.Context(), user.GetID(), &TwoFAState{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	unverified, err := sessions.Create(t.Context(), user.GetID(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := sessions.Create(t.Context(), user.GetID(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := sessions.MarkTwoFactorVerified(t.Context(), verified.Token); err != nil {
		t.Fatal(err)
	}
	lookups := 0
	users := manager.UserStore()
	manager.SetUserStore(&fakeUserStore{findByID: func(ctx context.Context, userID string) (User, error) {
		lookups++
		if lookups > 1 {
			return nil, errors.New("a later user lookup failed")
		}
		return users.FindByID(ctx, userID)
	}})
	outerContext := inProcessOuterContext(t, manager, unverified.Token, verified.Token)
	application := inProcessAssuranceApp(manager, twofa)
	recorder := httptest.NewRecorder()
	application.InProcessHandler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/protected", nil).WithContext(outerContext))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("a later cookie re-resolved the established user: %d %s", recorder.Code, recorder.Body.String())
	}
	if lookups != 1 {
		t.Fatalf("user lookups = %d, want 1", lookups)
	}
}

func TestInProcessTwoFAAssuranceNetworkBoundary(t *testing.T) {
	manager, twofa, sessions, user := newInProcessAssuranceRig(t)
	session, err := sessions.Create(t.Context(), user.GetID(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	outerContext := inProcessOuterContext(t, manager, session.Token)
	outerContext = context.WithValue(outerContext, sessionAssuranceKey{}, sessionAssurance{manager: manager, userID: user.GetID(), verified: true})
	application := inProcessAssuranceApp(manager, twofa)
	recorder := httptest.NewRecorder()
	application.Router().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/twofa-only", nil).WithContext(outerContext))
	if recorder.Code != http.StatusUnauthorized || !strings.Contains(recorder.Body.String(), `"reason":"no_session"`) {
		t.Fatalf("network trusted context without cookie: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestInProcessTwoFAAssuranceNestedAnonymous(t *testing.T) {
	manager, twofa, sessions, user := newInProcessAssuranceRig(t)
	if err := twofa.store.SetTwoFA(t.Context(), user.GetID(), &TwoFAState{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	session, err := sessions.Create(t.Context(), user.GetID(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := sessions.MarkTwoFactorVerified(t.Context(), session.Token); err != nil {
		t.Fatal(err)
	}
	outerContext := inProcessOuterContext(t, manager, session.Token)
	other, _, _, _ := newInProcessAssuranceRig(t)
	application := inProcessAssuranceApp(manager, twofa)
	inner := SessionMiddleware(other)(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if GetCurrentUser(request.Context()) != nil {
			t.Fatal("inner anonymous SessionMiddleware retained outer principal")
		}
		restored := handler.SetUser(request.Context(), user)
		restoredRecorder := httptest.NewRecorder()
		application.InProcessHandler().ServeHTTP(restoredRecorder, httptest.NewRequest(http.MethodGet, "/twofa-only", nil).WithContext(restored))
		if restoredRecorder.Code != http.StatusUnauthorized {
			t.Errorf("inner anonymous SessionMiddleware retained outer assurance: %d", restoredRecorder.Code)
		}
		for _, operation := range []struct {
			method string
			path   string
			status int
		}{
			{http.MethodGet, "/twofa-only", http.StatusUnauthorized},
			{http.MethodPost, "/auth/logout", http.StatusForbidden},
			{http.MethodPost, "/auth/2fa/disable", http.StatusForbidden},
		} {
			recorder := httptest.NewRecorder()
			application.InProcessHandler().ServeHTTP(recorder, httptest.NewRequest(operation.method, operation.path, nil).WithContext(request.Context()))
			if recorder.Code != operation.status {
				t.Errorf("%s = %d, want %d: %s", operation.path, recorder.Code, operation.status, recorder.Body.String())
			}
		}
	}))
	inner.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/inner", nil).WithContext(outerContext))
	if _, err := sessions.Get(t.Context(), session.Token); err != nil {
		t.Fatalf("Local logout revoked outer session: %v", err)
	}
	state, err := twofa.store.GetTwoFA(t.Context(), user.GetID())
	if err != nil || state == nil || !state.Enabled {
		t.Fatalf("Local disable changed outer factor: %+v, %v", state, err)
	}
}

func TestInProcessTwoFAAssuranceSnapshot(t *testing.T) {
	manager, twofa, sessions, user := newInProcessAssuranceRig(t)
	if err := twofa.store.SetTwoFA(t.Context(), user.GetID(), &TwoFAState{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	session, err := sessions.Create(t.Context(), user.GetID(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := sessions.MarkTwoFactorVerified(t.Context(), session.Token); err != nil {
		t.Fatal(err)
	}
	outerContext := inProcessOuterContext(t, manager, session.Token)
	if err := sessions.Delete(t.Context(), session.Token); err != nil {
		t.Fatal(err)
	}
	application := inProcessAssuranceApp(manager, twofa)
	recorder := httptest.NewRecorder()
	application.InProcessHandler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/protected", nil).WithContext(outerContext))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("Local assurance was reloaded as a credential: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestInProcessTwoFAAssuranceResolutionFailure(t *testing.T) {
	manager, twofa, sessions, user := newInProcessAssuranceRig(t)
	session, err := sessions.Create(t.Context(), user.GetID(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	outerContext := inProcessOuterContext(t, manager, session.Token)
	application := inProcessAssuranceApp(manager, twofa)
	for _, test := range []struct {
		name       string
		session    *Session
		sessionErr error
		user       User
		userErr    error
	}{
		{"SessionStoreError", nil, errors.New("store unavailable"), user, nil},
		{"MissingSession", nil, ErrSessionNotFound, user, nil},
		{"NilSession", nil, nil, user, nil},
		{"PendingSession", &Session{UserID: user.GetID(), PendingTwoFactor: true}, nil, user, nil},
		{"UserStoreError", &Session{UserID: user.GetID()}, nil, nil, errors.New("store unavailable")},
		{"MissingUser", &Session{UserID: user.GetID()}, nil, nil, ErrUserNotFound},
		{"NilUser", &Session{UserID: user.GetID()}, nil, nil, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			innerManager := newTestManagerWithStores(t,
				&fakeSessionStore{get: func(context.Context, string) (*Session, error) {
					return test.session, test.sessionErr
				}},
				&fakeUserStore{findByID: func(context.Context, string) (User, error) {
					return test.user, test.userErr
				}},
			)
			inner := SessionMiddleware(innerManager, WithSessionLogger(nil))(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if GetCurrentUser(request.Context()) != nil {
					t.Error("failed resolution kept the outer user")
				}
				restored := handler.SetUser(request.Context(), user)
				application.InProcessHandler().ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/twofa-only", nil).WithContext(restored))
			}))
			request := httptest.NewRequest(http.MethodGet, "/inner", nil).WithContext(outerContext)
			request.AddCookie(&http.Cookie{Name: innerManager.Config().SessionCookie, Value: "inner"})
			recorder := httptest.NewRecorder()
			inner.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("failed resolution kept the outer assurance: %d %s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestInProcessRequireSessionTrustsContextUser(t *testing.T) {
	manager, twofa, _, user := newInProcessAssuranceRig(t)
	application := inProcessAssuranceApp(manager, twofa)
	request := httptest.NewRequest(http.MethodGet, "/session-only", nil)
	request = request.WithContext(handler.SetUser(request.Context(), user))
	recorder := httptest.NewRecorder()
	application.InProcessHandler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("RequireSession re-read a session on a marked call: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestInProcessAccountOperations(t *testing.T) {
	manager, twofa, sessions, user := newInProcessAssuranceRig(t)
	_, tokens, _ := newTokenTestDB(t)
	plugins := []AuthPlugin{
		NewAccountsPlugin(),
		NewTokensPlugin(tokens),
		NewPasskeyPlugin(PasskeyConfig{RPID: "example.com", Origins: []string{"https://example.com"}, TwoFactorURL: "/2fa"}),
		NewOAuth2Plugin(OAuth2Config{TwoFactorURL: "/2fa"}),
		NewEmailVerificationPlugin(EmailVerificationConfig{BaseURL: "https://example.com"}),
		NewPasswordResetPlugin(PasswordResetConfig{BaseURL: "https://example.com"}),
		NewMagicLinkPlugin(MagicLinkConfig{BaseURL: "https://example.com", TwoFactorURL: "/2fa"}),
	}
	application := inProcessAssuranceApp(manager, twofa)
	for _, plugin := range plugins {
		if err := plugin.Init(manager); err != nil {
			t.Fatal(err)
		}
		plugin.(AuthPluginRoutes).RegisterRoutes(application.Router(), manager.Config().BasePath)
		switch gate := plugin.(type) {
		case *AccountsPlugin:
			application.Router().Get("/account-gate/accounts", http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if _, ok := gate.requireUserID(writer, request); ok {
					writer.WriteHeader(http.StatusNoContent)
				}
			}))
		case *TokensPlugin:
			application.Router().Get("/account-gate/tokens", http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if _, ok := gate.requireSessionUserID(writer, request); ok {
					writer.WriteHeader(http.StatusNoContent)
				}
			}))
		case *PasskeyPlugin:
			application.Router().Get("/account-gate/passkeys", http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if _, ok := gate.requireFullSession(writer, request); ok {
					writer.WriteHeader(http.StatusNoContent)
				}
			}))
		case *OAuth2Plugin:
			application.Router().Get("/account-gate/oauth", http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if _, ok := gate.requireSessionUserID(writer, request); ok {
					writer.WriteHeader(http.StatusNoContent)
				}
			}))
		}
		if stopper, ok := plugin.(AuthPluginOnStop); ok {
			t.Cleanup(func() {
				if err := stopper.OnStop(context.Background()); err != nil {
					t.Error(err)
				}
			})
		}
	}
	application.Router().Post("/step-up", http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if _, ok := twofa.requireStepUpSession(writer, request); ok {
			writer.WriteHeader(http.StatusNoContent)
		}
	}))
	session, err := sessions.Create(t.Context(), user.GetID(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	outerContext := inProcessOuterContext(t, manager, session.Token)
	for _, test := range []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/auth/login"},
		{http.MethodPost, "/auth/register"},
		{http.MethodPost, "/auth/logout"},
		{http.MethodGet, "/auth/me"},
		{http.MethodPost, "/auth/password"},
		{http.MethodPost, "/auth/2fa/enroll"},
		{http.MethodPost, "/auth/2fa/verify"},
		{http.MethodPost, "/auth/2fa/challenge"},
		{http.MethodPost, "/auth/2fa/disable"},
		{http.MethodPost, "/auth/2fa/backup-codes"},
		{http.MethodPost, "/step-up"},
		{http.MethodGet, "/account-gate/accounts"},
		{http.MethodGet, "/account-gate/tokens"},
		{http.MethodGet, "/account-gate/passkeys"},
		{http.MethodGet, "/account-gate/oauth"},
		{http.MethodPost, "/auth/passkey/login/options"},
		{http.MethodPost, "/auth/passkey/login"},
		{http.MethodPost, "/auth/passkey/register/options"},
		{http.MethodPost, "/auth/passkey/register"},
		{http.MethodGet, "/auth/passkey/list"},
		{http.MethodPost, "/auth/passkey/delete"},
		{http.MethodPost, "/auth/tokens"},
		{http.MethodGet, "/auth/tokens"},
		{http.MethodDelete, "/auth/tokens/token-id"},
		{http.MethodGet, "/auth/oauth/provider"},
		{http.MethodGet, "/auth/oauth/provider/link"},
		{http.MethodGet, "/auth/oauth/provider/callback"},
		{http.MethodGet, "/auth/accounts"},
		{http.MethodDelete, "/auth/unlink/provider"},
		{http.MethodPost, "/auth/send-verification"},
		{http.MethodGet, "/auth/verify-email"},
		{http.MethodPost, "/auth/forgot-password"},
		{http.MethodPost, "/auth/reset-password"},
		{http.MethodPost, "/auth/magic-link/send"},
		{http.MethodGet, "/auth/magic-link/verify"},
		{http.MethodPost, "/auth/magic-link/verify"},
	} {
		t.Run(test.method+test.path, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, strings.NewReader(`{}`)).WithContext(outerContext)
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			application.InProcessHandler().ServeHTTP(recorder, request)
			if recorder.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403: %s", recorder.Code, recorder.Body.String())
			}
		})
	}
}
