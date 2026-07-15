package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/mcp"
)

type gateUser struct{ roles []string }

func (u *gateUser) GetID() string      { return "u1" }
func (u *gateUser) GetEmail() string   { return "u1@example.com" }
func (u *gateUser) GetRoles() []string { return u.roles }

// The full wire: /mcp sits under SessionMiddleware like every other
// route, so a Gated custom tool sees the resolved caller. Cookie in →
// tool runs; anonymous → JSON-RPC error, handler never runs.
func TestGatedCustomToolOverTheWire(t *testing.T) {
	mgr := newTestManagerWithStores(t,
		&fakeSessionStore{get: func(_ context.Context, token string) (*Session, error) {
			if token == "tok" {
				return &Session{Token: "tok", UserID: "u1", ExpiresAt: time.Now().Add(time.Hour)}, nil
			}
			return nil, ErrSessionNotFound
		}},
		&fakeUserStore{findByID: func(_ context.Context, id string) (User, error) {
			return &gateUser{roles: []string{"admin"}}, nil
		}},
	)

	server := mcp.NewServer()
	ran := false
	err := server.RegisterTool("reports_rebuild", "rebuild reports", map[string]any{"type": "object"},
		mcp.Gated(MCPRole("admin"), func(ctx context.Context, params map[string]any) (any, error) {
			ran = true
			return map[string]any{"ok": true}, nil
		}))
	if err != nil {
		t.Fatal(err)
	}
	h := SessionMiddleware(mgr)(server)

	call := func(cookie string) (status int, body string) {
		req := httptest.NewRequest(http.MethodPost, "/mcp",
			strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"reports_rebuild","arguments":{}}}`))
		req.Header.Set("Content-Type", "application/json")
		if cookie != "" {
			req.Header.Set("Cookie", "test_session="+cookie)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}

	// Anonymous: refused, handler untouched.
	_, body := call("")
	var resp struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("decode: %v\n%s", err, body)
	}
	if resp.Error == nil || !strings.Contains(resp.Error.Message, "authenticated caller") {
		t.Fatalf("anonymous call must return the gate error, got: %s", body)
	}
	if ran {
		t.Fatal("handler ran for anonymous caller")
	}

	// Authenticated admin: runs.
	_, body = call("tok")
	if strings.Contains(body, `"error"`) {
		t.Fatalf("authenticated admin refused: %s", body)
	}
	if !ran {
		t.Fatal("handler did not run for authenticated admin")
	}
}

func TestMCPRoleRefusesWrongRole(t *testing.T) {
	gate := MCPRole("admin")
	ctx := handler.SetUser(context.Background(), &gateUser{roles: []string{"viewer"}})
	if err := gate(ctx); err == nil || !strings.Contains(err.Error(), "requires role") {
		t.Fatalf("want role refusal, got %v", err)
	}
	if err := MCPUser()(ctx); err != nil {
		t.Fatalf("MCPUser should accept any authenticated user, got %v", err)
	}
}
