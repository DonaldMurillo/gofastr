package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Scope rejections use the canonical flat error envelope
// {"error": "<message>", "success": false, "code": 403} with a JSON
// content type, the shape the generated SDKs and sdkdocs document.
func TestScopeErrorFlatEnvelope(t *testing.T) {
	reject := func(t *testing.T, h http.Handler, req *http.Request) {
		t.Helper()
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Fatalf("Content-Type = %q, want application/json", ct)
		}
		var envelope struct {
			Error   any   `json:"error"`
			Success *bool `json:"success"`
			Code    int   `json:"code"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("body is not JSON: %v (%q)", err, rec.Body.String())
		}
		msg, ok := envelope.Error.(string)
		if !ok || msg == "" {
			t.Fatalf(`"error" = %#v, want non-empty string (flat envelope)`, envelope.Error)
		}
		if envelope.Success == nil || *envelope.Success {
			t.Fatalf(`"success" = %v, want false`, envelope.Success)
		}
		if envelope.Code != http.StatusForbidden {
			t.Fatalf(`"code" = %d, want 403`, envelope.Code)
		}
	}

	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	t.Run("RequireAPIScopes", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/customers", nil)
		req = req.WithContext(context.WithValue(req.Context(), tokenScopesKey{}, []string{"invoices:read"}))
		reject(t, RequireAPIScopes("/api")(ok), req)
	})
	t.Run("RequireScope", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/anything", nil)
		req = req.WithContext(context.WithValue(req.Context(), tokenScopesKey{}, []string{"other:read"}))
		reject(t, RequireScope("posts:read")(ok), req)
	})
}

// Scope envelope across every store read surface.
//
// Property: the scope set minted at issue is the scope set observed at
// EVERY read surface — FindByHash (the middleware's authority),
// List (the owner's self-service view), and ListAll (the admin view). A
// surface that drops, adds, or mangles scopes changes what a token may
// do depending on which code path looks at it.
func TestScopesRoundTripEveryReadSurface(t *testing.T) {
	shapes := []struct {
		name   string
		scopes []string
	}{
		{"nil scopes", nil},
		{"empty scopes", []string{}},
		{"single scope", []string{"customers:read"}},
		{"multiple scopes", []string{"customers:*", "invoices:read"}},
	}
	for _, shape := range shapes {
		t.Run(shape.name, func(t *testing.T) {
			_, ts, _ := newTokenTestDB(t)
			ctx := context.Background()
			pt, rec, err := IssueToken(ctx, ts, TokenSpec{
				Name: "t-" + shape.name, OwnerKind: "user", OwnerID: "u1", Scopes: shape.scopes,
			})
			if err != nil {
				t.Fatal(err)
			}
			want := strings.Join(shape.scopes, "\x00")
			surfaces := []struct {
				name   string
				scopes func() ([]string, error)
			}{
				{"FindByHash", func() ([]string, error) {
					tok, err := ts.FindByHash(ctx, sha256hex(pt))
					if err != nil || tok == nil {
						return nil, fmt.Errorf("FindByHash: %v %v", tok, err)
					}
					return tok.Scopes, nil
				}},
				{"List", func() ([]string, error) {
					toks, err := ts.List(ctx, "user", "u1")
					if err != nil || len(toks) != 1 {
						return nil, fmt.Errorf("List: %d toks, err %v", len(toks), err)
					}
					return toks[0].Scopes, nil
				}},
				{"ListAll", func() ([]string, error) {
					toks, err := ts.ListAll(ctx)
					if err != nil || len(toks) != 1 {
						return nil, fmt.Errorf("ListAll: %d toks, err %v", len(toks), err)
					}
					return toks[0].Scopes, nil
				}},
			}
			for _, s := range surfaces {
				got, err := s.scopes()
				if err != nil {
					t.Fatalf("%s: %v", s.name, err)
				}
				if strings.Join(got, "\x00") != want {
					t.Errorf("SECURITY: [token-scope-envelope] %s returned scopes %q for token %s minted with %q — the scope envelope differs across read surfaces", s.name, got, rec.ID, shape.scopes)
				}
			}
		})
	}
}

// Unrelated writes must not disturb the envelope: TouchLastUsed and
// Revoke stamp their OWN columns; after both, a FindByHash must still
// report the minted scopes and the revocation.
func TestTouchRevokeKeepScopeEnvelope(t *testing.T) {
	_, ts, _ := newTokenTestDB(t)
	ctx := context.Background()
	want := []string{"customers:read", "invoices:write"}
	pt, rec, err := IssueToken(ctx, ts, TokenSpec{
		Name: "t", OwnerKind: "user", OwnerID: "u1", Scopes: want,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := ts.TouchLastUsed(ctx, rec.ID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := ts.Revoke(ctx, rec.ID, "user", "u1"); err != nil {
		t.Fatal(err)
	}
	tok, err := ts.FindByHash(ctx, sha256hex(pt))
	if err != nil || tok == nil {
		t.Fatalf("FindByHash after touch+revoke: %v %v", tok, err)
	}
	if strings.Join(tok.Scopes, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("SECURITY: [token-scope-envelope] scopes after TouchLastUsed+Revoke = %q, want %q", tok.Scopes, want)
	}
	if tok.RevokedAt == nil {
		t.Error("revoke did not stamp RevokedAt alongside the touch")
	}
}

// A corrupt scopes column (hand-edited row, partial write, or a host
// writing the table directly) must degrade to "no scopes" at every read
// surface — never a panic, never an error that 500s the middleware path,
// and never a value an attacker chose.
func TestCorruptScopesColumnNoPanicEverySurface(t *testing.T) {
	db, ts, _ := newTokenTestDB(t)
	ctx := context.Background()
	pt, rec, err := IssueToken(ctx, ts, TokenSpec{
		Name: "t", OwnerKind: "user", OwnerID: "u1", Scopes: []string{"a:read"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE auth_api_tokens SET scopes = 'not-json{' WHERE id = $1`, rec.ID); err != nil {
		t.Fatal(err)
	}

	assertEmpty := func(name string, got []string, err error) {
		t.Helper()
		if err != nil {
			t.Errorf("SECURITY: [token-scope-envelope] %s errored on a corrupt scopes column: %v — the read path must degrade, not 500", name, err)
		}
		if len(got) != 0 {
			t.Errorf("SECURITY: [token-scope-envelope] %s minted scopes from a corrupt column: %q — an attacker-chosen row value became the token's authority", name, got)
		}
	}
	tok, err := ts.FindByHash(ctx, sha256hex(pt))
	if err == nil && tok != nil {
		assertEmpty("FindByHash", tok.Scopes, nil)
	} else {
		assertEmpty("FindByHash", nil, err)
	}
	listed, err := ts.List(ctx, "user", "u1")
	if err == nil && len(listed) == 1 {
		assertEmpty("List", listed[0].Scopes, nil)
	} else {
		assertEmpty("List", nil, err)
	}
	all, err := ts.ListAll(ctx)
	if err == nil && len(all) == 1 {
		assertEmpty("ListAll", all[0].Scopes, nil)
	} else {
		assertEmpty("ListAll", nil, err)
	}
}
