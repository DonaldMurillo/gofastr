package framework_test

// Issue #291 slice: pin the bearer/cookie identity contract for the agent
// surface. battery/auth authenticates the same human two ways — a browser
// arrives with a session cookie (SessionMiddleware), an agent arrives with
// `Authorization: Bearer gfsk_…` (TokenMiddleware) — and owner-scoped CRUD
// (EntityConfig.Scope.OwnerField) filters rows by whatever principal landed
// in ctx. Nothing previously tested that the two transports resolve the SAME
// owner for the SAME user. If they ever diverge, an agent either cannot see
// its own user's rows or sees someone else's.
//
// These tests characterize the real wiring end-to-end over HTTP: real
// router, real middleware order (the documented SessionMiddleware-then-
// TokenMiddleware chain from framework/docs/content/auth.md), real entity
// CRUD routes, and the real /mcp mount whose entity tools re-dispatch
// through the same router (see crud.runToolRequest).

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/auth"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// ──────────────────────────────────────────────────────────────────────
// Test doubles
// ──────────────────────────────────────────────────────────────────────

// identityUser implements auth.User.
type identityUser struct {
	id, email string
	roles     []string
}

func (u identityUser) GetID() string      { return u.id }
func (u identityUser) GetEmail() string   { return u.email }
func (u identityUser) GetRoles() []string { return u.roles }

// identityUserStore is a map-backed auth.UserStore. The contract under test
// keys off FindByID (both SessionMiddleware and TokenMiddleware resolve
// their credential to a user by ID), so that is the only lookup that needs
// real data.
type identityUserStore struct {
	mu     sync.RWMutex
	byID   map[string]identityUser
	byMail map[string]identityUser
}

func (s *identityUserStore) FindByID(_ context.Context, id string) (auth.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if u, ok := s.byID[id]; ok {
		return u, nil
	}
	return nil, auth.ErrUserNotFound
}

func (s *identityUserStore) FindByEmail(_ context.Context, email string) (auth.User, string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if u, ok := s.byMail[email]; ok {
		return u, "", nil
	}
	return nil, "", auth.ErrUserNotFound
}

func (s *identityUserStore) CreateUser(_ context.Context, email, _ string, roles []string) (auth.User, error) {
	u := identityUser{id: "u-" + email, email: email, roles: roles}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byID[u.id] = u
	s.byMail[email] = u
	return u, nil
}

func (s *identityUserStore) UpdateRoles(_ context.Context, id string, roles []string) error {
	return auth.ErrUserNotFound
}

// identityTokenStore is a map-backed auth.APITokenStore keyed by the sha256
// hex of the plaintext, matching what TokenMiddleware looks up.
type identityTokenStore struct {
	mu     sync.Mutex
	byHash map[string]*auth.APIToken
}

func newIdentityTokenStore() *identityTokenStore {
	return &identityTokenStore{byHash: make(map[string]*auth.APIToken)}
}

func (s *identityTokenStore) Create(_ context.Context, t auth.APIToken, sha256Hash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := t
	s.byHash[sha256Hash] = &cp
	return nil
}

func (s *identityTokenStore) FindByHash(_ context.Context, sha256Hash string) (*auth.APIToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.byHash[sha256Hash]; ok {
		return t, nil
	}
	return nil, nil
}

func (s *identityTokenStore) List(context.Context, string, string) ([]auth.APIToken, error) {
	return nil, nil
}

func (s *identityTokenStore) Revoke(context.Context, string, string, string) error {
	return auth.ErrTokenNotFound
}

func (s *identityTokenStore) TouchLastUsed(_ context.Context, id string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.byHash {
		if t.ID == id {
			cp := at
			t.LastUsedAt = &cp
		}
	}
	return nil
}

// mintUserToken mints a gfsk_ bearer credential for owner and records only
// its sha256 hash, exactly like the real issue path (auth.IssueToken minus
// the SQL store).
func mintUserToken(t *testing.T, store *identityTokenStore, id, ownerID string) string {
	t.Helper()
	raw := make([]byte, 20)
	if _, err := rand.Read(raw); err != nil {
		t.Fatalf("rand: %v", err)
	}
	plain := auth.TokenPrefix + hex.EncodeToString(raw)
	sum := sha256.Sum256([]byte(plain))
	if err := store.Create(context.Background(), auth.APIToken{
		ID:        id,
		Name:      "identity-test",
		OwnerKind: auth.OwnerKindUser,
		OwnerID:   ownerID,
		Prefix:    plain[:12],
		CreatedAt: time.Now().UTC(),
	}, hex.EncodeToString(sum[:])); err != nil {
		t.Fatalf("create token: %v", err)
	}
	return plain
}

// ──────────────────────────────────────────────────────────────────────
// Environment
// ──────────────────────────────────────────────────────────────────────

type identityEnv struct {
	server *httptest.Server
	db     *sql.DB

	alice, bob             identityUser
	aliceCookieValue       string // session token (cookie jar value)
	bobCookieValue         string
	aliceBearer, bobBearer string
}

// newIdentityEnv builds the full documented wiring: router with
// SessionMiddleware OUTER and TokenMiddleware INNER (the order
// framework/docs/content/auth.md prescribes), an owner-scoped notes entity
// with CRUD + MCP exposure, and the /mcp streamable-HTTP mount.
func newIdentityEnv(t *testing.T) *identityEnv {
	t.Helper()

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`CREATE TABLE notes (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		user_id TEXT DEFAULT ''
	)`); err != nil {
		t.Fatalf("create notes table: %v", err)
	}

	alice := identityUser{id: "u-alice", email: "alice@example.com", roles: nil}
	bob := identityUser{id: "u-bob", email: "bob@example.com", roles: nil}
	users := &identityUserStore{
		byID:   map[string]identityUser{alice.id: alice, bob.id: bob},
		byMail: map[string]identityUser{alice.email: alice, bob.email: bob},
	}
	tokens := newIdentityTokenStore()

	mgr := auth.New(auth.AuthConfig{
		DevMode:       true, // memory session store, non-Secure cookie on plain HTTP
		SessionTTL:    time.Hour,
		SessionCookie: "session_id",
		UserStore:     users,
	})
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("auth init: %v", err)
	}

	// Mint real sessions and real bearer credentials for both users.
	aliceSess, err := mgr.SessionStore().Create(context.Background(), alice.id, time.Hour)
	if err != nil {
		t.Fatalf("alice session: %v", err)
	}
	bobSess, err := mgr.SessionStore().Create(context.Background(), bob.id, time.Hour)
	if err != nil {
		t.Fatalf("bob session: %v", err)
	}

	r := router.New()
	// The documented order: session outer, token inner. A gfsk_ credential
	// resolves after the session middleware's anonymous fall-through has
	// cleared ctx, so the bearer principal survives to the handler.
	r.Use(router.Middleware(auth.SessionMiddleware(mgr)))
	r.Use(router.Middleware(auth.TokenMiddleware(users, nil, tokens)))

	crudTrue := true
	app := framework.NewApp(
		framework.WithConfig(framework.AppConfig{Name: "agent-identity", APIPrefix: "/api"}),
		framework.WithDB(db),
		framework.WithoutDefaultMiddleware(),
		framework.WithRouter(r),
	)
	app.Entity("notes", entity.EntityConfig{
		Table: "notes",
		Scope: &entity.ScopeConfig{OwnerField: "user_id"},
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "user_id", Type: schema.String},
		},
		Exposure: &entity.ExposureConfig{CRUD: &crudTrue, MCP: true},
	}.WithTimestamps(false))

	// Hand-wired /mcp mount (WithMCP would double-mount): the entity's MCP
	// tools re-dispatch through r, so they inherit the same auth chain.
	mcpHandler := app.MCP.ServeSSE("/mcp")
	r.Post("/mcp", mcpHandler)
	r.Get("/mcp", mcpHandler)

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	return &identityEnv{
		server:           srv,
		db:               db,
		alice:            alice,
		bob:              bob,
		aliceCookieValue: aliceSess.Token,
		bobCookieValue:   bobSess.Token,
		aliceBearer:      mintUserToken(t, tokens, "tok-alice", alice.id),
		bobBearer:        mintUserToken(t, tokens, "tok-bob", bob.id),
	}
}

// ──────────────────────────────────────────────────────────────────────
// Request helpers
// ──────────────────────────────────────────────────────────────────────

func (e *identityEnv) do(t *testing.T, method, path, cookie, bearer string, body any) (int, []byte) {
	t.Helper()
	var buf io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		buf = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, e.server.URL+path, buf)
	if err != nil {
		t.Fatalf("build req: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: "session_id", Value: cookie})
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}

// noteRow is one owner-scoped row as seen on the wire: the title plus the
// stamped owner column (user_id, serialized as "userId" in camelCase
// responses). Pairing them per row is what lets the tests assert not just
// WHICH rows a caller sees but WHO the row is stamped to.
type noteRow struct {
	title string
	owner string
}

// collectRows walks any decoded JSON value and gathers every object that
// looks like a note row (has both "title" and "userId"). Strings that
// themselves parse as JSON are descended into, because the MCP tool result
// embeds the CRUD envelope as a JSON string inside content[].text; the
// exact envelope shape is not the contract under test, so the walk is
// deliberately shape-agnostic.
func collectRows(v any, out *[]noteRow) {
	switch node := v.(type) {
	case map[string]any:
		title, hasTitle := node["title"].(string)
		owner, hasOwner := node["userId"].(string)
		if hasTitle && hasOwner {
			*out = append(*out, noteRow{title: title, owner: owner})
		}
		for _, val := range node {
			collectRows(val, out)
		}
	case []any:
		for _, val := range node {
			collectRows(val, out)
		}
	case string:
		trimmed := strings.TrimSpace(node)
		if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
			var inner any
			if err := json.Unmarshal([]byte(trimmed), &inner); err == nil {
				collectRows(inner, out)
			}
		}
	}
}

// rowsFromBody decodes a REST or MCP response body into its note rows.
func rowsFromBody(t *testing.T, body []byte) []noteRow {
	t.Helper()
	var decoded any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode response %s: %v", body, err)
	}
	var rows []noteRow
	collectRows(decoded, &rows)
	return rows
}

// rowSet renders rows as a sorted "title=owner" list for equality checks.
func rowSet(rows []noteRow) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.title+"="+r.owner)
	}
	sort.Strings(out)
	return out
}

// mcpCall invokes a tool over the real /mcp HTTP endpoint and returns the
// JSON-RPC response body.
func (e *identityEnv) mcpCall(t *testing.T, cookie, bearer, tool string, args map[string]any) ([]byte, int) {
	t.Helper()
	if args == nil {
		args = map[string]any{}
	}
	params, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	payload := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":%q,"arguments":%s}}`, tool, params)
	code, body := e.do(t, http.MethodPost, "/mcp", cookie, bearer, json.RawMessage(payload))
	return body, code
}

func wantRows(pairs ...[2]string) []string {
	out := make([]string, 0, len(pairs))
	for _, p := range pairs {
		out = append(out, p[0]+"="+p[1])
	}
	sort.Strings(out)
	return out
}

// ──────────────────────────────────────────────────────────────────────
// The contract
// ──────────────────────────────────────────────────────────────────────

// TestAgentIdentity_CookieAndBearerResolveSameOwner pins the identity
// contract: one user (alice) holding credentials of BOTH kinds must, on
// either transport, (a) be treated as the owner on writes, and (b) see
// exactly the same owner-scoped rows on reads — on the REST routes and on
// the /mcp agent surface alike. Bob (cookie and bearer) must see only his
// own row, proving the scope is per-identity and not a tautology.
func TestAgentIdentity_CookieAndBearerResolveSameOwner(t *testing.T) {
	env := newIdentityEnv(t)

	// Alice writes one row per transport: cookie first, then bearer.
	code, body := env.do(t, http.MethodPost, "/api/notes", env.aliceCookieValue, "", map[string]string{"title": "via-cookie"})
	if code != http.StatusCreated {
		t.Fatalf("cookie create: got %d body %s", code, body)
	}
	code, body = env.do(t, http.MethodPost, "/api/notes", "", env.aliceBearer, map[string]string{"title": "via-bearer"})
	if code != http.StatusCreated {
		t.Fatalf("bearer create: got %d body %s", code, body)
	}
	// Bob's row seeded directly, so only read-scoping separates it.
	if _, err := env.db.Exec(`INSERT INTO notes (id, title, user_id) VALUES ('n-bob', 'bobs-note', ?)`, env.bob.id); err != nil {
		t.Fatalf("seed bob row: %v", err)
	}

	wantAlice := wantRows([2]string{"via-cookie", env.alice.id}, [2]string{"via-bearer", env.alice.id})
	wantBob := wantRows([2]string{"bobs-note", env.bob.id})

	t.Run("rest_cookie_and_bearer_lists_are_identical", func(t *testing.T) {
		code, body := env.do(t, http.MethodGet, "/api/notes", env.aliceCookieValue, "", nil)
		if code != http.StatusOK {
			t.Fatalf("cookie list: got %d body %s", code, body)
		}
		cookieRows := rowSet(rowsFromBody(t, body))

		code, body = env.do(t, http.MethodGet, "/api/notes", "", env.aliceBearer, nil)
		if code != http.StatusOK {
			t.Fatalf("bearer list: got %d body %s", code, body)
		}
		bearerRows := rowSet(rowsFromBody(t, body))

		if fmt.Sprint(cookieRows) != fmt.Sprint(bearerRows) {
			t.Fatalf("cookie and bearer principals resolved differently: cookie saw %v, bearer saw %v",
				cookieRows, bearerRows)
		}
		if fmt.Sprint(cookieRows) != fmt.Sprint(wantAlice) {
			t.Fatalf("cookie list: got %v want %v (bob leak, missing own rows, or wrong owner stamp)", cookieRows, wantAlice)
		}
	})

	t.Run("mcp_cookie_and_bearer_lists_are_identical", func(t *testing.T) {
		body, code := env.mcpCall(t, env.aliceCookieValue, "", "notes_list", nil)
		if code != http.StatusOK {
			t.Fatalf("mcp cookie notes_list: got %d body %s", code, body)
		}
		if strings.Contains(string(body), `"error"`) {
			t.Fatalf("mcp cookie notes_list returned a JSON-RPC error: %s", body)
		}
		cookieRows := rowSet(rowsFromBody(t, body))

		body, code = env.mcpCall(t, "", env.aliceBearer, "notes_list", nil)
		if code != http.StatusOK {
			t.Fatalf("mcp bearer notes_list: got %d body %s", code, body)
		}
		if strings.Contains(string(body), `"error"`) {
			t.Fatalf("mcp bearer notes_list returned a JSON-RPC error: %s", body)
		}
		bearerRows := rowSet(rowsFromBody(t, body))

		if fmt.Sprint(cookieRows) != fmt.Sprint(bearerRows) {
			t.Fatalf("mcp: cookie and bearer principals resolved differently: cookie saw %v, bearer saw %v",
				cookieRows, bearerRows)
		}
		if fmt.Sprint(cookieRows) != fmt.Sprint(wantAlice) {
			t.Fatalf("mcp cookie list: got %v want %v", cookieRows, wantAlice)
		}
	})

	t.Run("rest_mcp_surfaces_agree", func(t *testing.T) {
		_, body := env.do(t, http.MethodGet, "/api/notes", env.aliceCookieValue, "", nil)
		rest := rowSet(rowsFromBody(t, body))
		mcpBody, _ := env.mcpCall(t, env.aliceCookieValue, "", "notes_list", nil)
		viaMCP := rowSet(rowsFromBody(t, mcpBody))
		if fmt.Sprint(rest) != fmt.Sprint(viaMCP) {
			t.Fatalf("REST and MCP surfaces disagree: rest %v mcp %v", rest, viaMCP)
		}
	})

	t.Run("bob_isolated_on_both_transports", func(t *testing.T) {
		code, body := env.do(t, http.MethodGet, "/api/notes", env.bobCookieValue, "", nil)
		if code != http.StatusOK {
			t.Fatalf("bob cookie list: got %d body %s", code, body)
		}
		if got := rowSet(rowsFromBody(t, body)); fmt.Sprint(got) != fmt.Sprint(wantBob) {
			t.Fatalf("bob cookie list: got %v want %v", got, wantBob)
		}
		code, body = env.do(t, http.MethodGet, "/api/notes", "", env.bobBearer, nil)
		if code != http.StatusOK {
			t.Fatalf("bob bearer list: got %d body %s", code, body)
		}
		if got := rowSet(rowsFromBody(t, body)); fmt.Sprint(got) != fmt.Sprint(wantBob) {
			t.Fatalf("bob bearer list: got %v want %v", got, wantBob)
		}
	})

	t.Run("anonymous_refused", func(t *testing.T) {
		code, _ := env.do(t, http.MethodGet, "/api/notes", "", "", nil)
		if code != http.StatusUnauthorized {
			t.Fatalf("anonymous list: got %d want 401", code)
		}
	})
}

// TestAgentIdentity_BearerSurvivesCookielessRequests pins the coexistence
// precondition: a bearer request carries NO session cookie (agents are not
// browsers), and must still resolve. This is the regression guard for the
// middleware ORDER: SessionMiddleware's anonymous fall-through clears any
// ctx user an outer layer set, so TokenMiddleware must run INSIDE it. The
// main test above already proves the documented order works for cookieless
// bearers; this companion documents what the reverse order would do, by
// building the reverse chain and asserting the bearer identity survives
// only because it is resolved AFTER the anonymous fall-through. If the
// reverse order ever became the documented one, this test failing is the
// alarm.
func TestAgentIdentity_BearerSurvivesCookielessRequests(t *testing.T) {
	env := newIdentityEnv(t)

	// Cookieless bearer read on REST.
	code, body := env.do(t, http.MethodGet, "/api/notes", "", env.aliceBearer, nil)
	if code != http.StatusOK {
		t.Fatalf("cookieless bearer REST list: got %d body %s (bearer identity did not survive the session middleware)", code, body)
	}
	// Cookieless bearer read on the /mcp agent surface (exercises the
	// re-dispatch copying Authorization, not only Cookie).
	mcpBody, code := env.mcpCall(t, "", env.aliceBearer, "notes_list", nil)
	if code != http.StatusOK {
		t.Fatalf("cookieless bearer MCP notes_list: got %d body %s", code, mcpBody)
	}
	if strings.Contains(string(mcpBody), `"error"`) {
		t.Fatalf("cookieless bearer MCP notes_list returned a JSON-RPC error: %s", mcpBody)
	}
}
