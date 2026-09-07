package main

// Pins: mutating the shared cross-tenant plans catalog requires an
// elevated grant, not mere session possession — REST and MCP writes are
// gated on the plans `access:` block (plans:write / plans:admin,
// resolved through the RolePolicy in app.go: the bootstrap admin's
// wildcard covers them, a plain signup session holds nothing), while
// reads stay open for the pricing pages. The docs' own doctrine
// (entity-declarations.md): "a session requirement is not row scoping…
// Without OwnerField, every authenticated user reads and can overwrite
// every other user's rows" — OwnerField is wrong for a shared catalog,
// so the gate is an Access policy.
//
// Finding (verified live, 2026-09-06, pre-fix): a fresh signup session
// PUT /api/plans/<pro> {"price":"0.01"} → 200 (price 99→0.01); POST →
// 201; DELETE → 204; MCP plans_update renamed the shared plan. Every
// paying customer's subscription pricing flows from this table.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/dotenv"
	"github.com/DonaldMurillo/gofastr/framework"
)

// plansGateUser registers + logs in a fresh, unprivileged account and returns
// a session-carrying client.
func plansGateUser(t *testing.T, base, email string) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("setup broken: cookiejar: %v", err)
	}
	client := &http.Client{Jar: jar}
	creds := `{"email":"` + email + `","password":"plans-gate-pass-123"}`
	if code, body := e2eDo(t, client, "POST", base+"/auth/register", creds); code >= 400 {
		t.Fatalf("setup broken: register %s = %d: %s", email, code, body)
	}
	if _, err := client.PostForm(base+"/auth/login", url.Values{"email": {email}, "password": {"plans-gate-pass-123"}}); err != nil {
		t.Fatalf("setup broken: login: %v", err)
	}
	return client
}

// plansGateAdmin logs in the bootstrap admin the same way TestMeridianTokenAuth does.
func plansGateAdmin(t *testing.T, base string) *http.Client {
	t.Helper()
	adminPass := os.Getenv("ADMIN_SEED_PASSWORD")
	if adminPass == "" {
		t.Fatal("setup broken: ADMIN_SEED_PASSWORD not set after e2eBootApp")
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("setup broken: cookiejar: %v", err)
	}
	client := &http.Client{Jar: jar}
	if _, err := client.PostForm(base+"/auth/login", url.Values{"email": {"admin@meridian.dev"}, "password": {adminPass}}); err != nil {
		t.Fatalf("setup broken: admin login: %v", err)
	}
	return client
}

// plansGatePro returns the seeded slug=pro plan row from the catalog.
func plansGatePro(t *testing.T, client *http.Client, base string) map[string]any {
	t.Helper()
	code, body := e2eDo(t, client, "GET", base+"/api/plans", "")
	if code != http.StatusOK {
		t.Fatalf("setup broken: GET /api/plans = %d: %s", code, body)
	}
	var list struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &list); err != nil {
		t.Fatalf("setup broken: decode plans list: %v\n%s", err, body)
	}
	for _, row := range list.Data {
		if row["slug"] == "pro" {
			return row
		}
	}
	t.Fatalf("setup broken: seeded pro plan not found in catalog:\n%s", body)
	return nil
}

func TestPlansGateSessionWriteRefused(t *testing.T) {
	if testing.Short() {
		t.Skip("builds + boots the binary")
	}
	_ = dotenv.LoadAndApply(framework.DefaultDotEnvPaths()...)
	base := e2eBootApp(t)

	pleb := plansGateUser(t, base, "pleb@plansgate.example")
	pro := plansGatePro(t, pleb, base)
	proID, _ := pro["id"].(string)
	if proID == "" {
		t.Fatalf("setup broken: pro plan row has no id: %#v", pro)
	}

	// PUT the shared row: must be refused AND leave the row untouched.
	if code, body := e2eDo(t, pleb, "PUT", base+"/api/plans/"+proID, `{"name":"tampered"}`); code < http.StatusForbidden {
		t.Errorf("SECURITY: [meridian-plans-world-mutable] plain-session PUT /api/plans/%s = %d: %s — the cross-tenant plans catalog is writable by any fresh signup (no OwnerField, no Access policy; the API-token leg is 403'd but sessions bypass RequireAPIScopes)", proID, code, body)
	}
	code, body := e2eDo(t, pleb, "GET", base+"/api/plans/"+proID, "")
	if code != http.StatusOK {
		t.Fatalf("setup broken: re-GET plan = %d: %s", code, body)
	}
	var after struct {
		Data struct {
			Name  string `json:"name"`
			Price any    `json:"price"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &after); err != nil {
		t.Fatalf("setup broken: decode re-read plan: %v\n%s", err, body)
	}
	if after.Data.Name != "Pro" || !strings.Contains(fmt.Sprint(after.Data.Price), "99") {
		t.Errorf("SECURITY: [meridian-plans-world-mutable] re-GET after refused-session-write shows name=%q price=%v, want the untouched seeded Pro/99 — a session write mutated the shared catalog", after.Data.Name, after.Data.Price)
	}

	// POST: creating catalog rows is equally a mutation.
	if code, body := e2eDo(t, pleb, "POST", base+"/api/plans", `{"name":"evil","slug":"evil-plan","price":"1"}`); code < http.StatusForbidden {
		t.Errorf("SECURITY: [meridian-plans-world-mutable] plain-session POST /api/plans = %d: %s — any fresh signup mints shared catalog rows", code, body)
	}

	// DELETE on a row the TEST created via admin (fresh row: no subscription
	// FK points at it, so only the missing write gate can answer).
	admin := plansGateAdmin(t, base)
	code, body = e2eDo(t, admin, "POST", base+"/api/plans", `{"name":"doomed","slug":"doomed-plan","price":"1"}`)
	if code != http.StatusCreated {
		t.Fatalf("setup broken: admin create doomed plan = %d: %s", code, body)
	}
	doomedID := e2eExtractID(body)
	if doomedID == "" {
		t.Fatalf("setup broken: no id in created plan: %s", body)
	}
	if code, body := e2eDo(t, pleb, "DELETE", base+"/api/plans/"+doomedID, ""); code < http.StatusForbidden {
		t.Errorf("SECURITY: [meridian-plans-world-mutable] plain-session DELETE /api/plans/%s = %d: %s — any fresh signup deletes shared catalog rows", doomedID, code, body)
	}

	// Contrast leg: the elevated grant (seeded admin) still writes the
	// catalog — the gate must be about elevation, not a locked table.
	if code, body := e2eDo(t, admin, "PUT", base+"/api/plans/"+proID, `{"name":"admin-contrast"}`); code/100 != 2 {
		t.Errorf("contrast broken: admin-session PUT /api/plans/%s = %d: %s — an elevated grant should still write the catalog", proID, code, body)
	}
}

func TestPlansGateMCPWriteRefused(t *testing.T) {
	if testing.Short() {
		t.Skip("builds + boots the binary")
	}
	_ = dotenv.LoadAndApply(framework.DefaultDotEnvPaths()...)
	base := e2eBootApp(t)

	pleb := plansGateUser(t, base, "mcppleb@plansgate.example")
	pro := plansGatePro(t, pleb, base)
	proID, _ := pro["id"].(string)
	if proID == "" {
		t.Fatalf("setup broken: pro plan row has no id: %#v", pro)
	}

	// plans_update over MCP with nothing but a plain session.
	payload := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"plans_update","arguments":{"id":%q,"name":"tampered"}}}`, proID)
	code, body := e2eDo(t, pleb, "POST", base+"/mcp", payload)
	var resp struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
		Result *struct {
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	_ = json.Unmarshal([]byte(body), &resp)
	if code < http.StatusBadRequest && resp.Error == nil && (resp.Result == nil || !resp.Result.IsError) {
		t.Errorf("SECURITY: [meridian-plans-world-mutable] MCP plans_update by a plain session succeeded (HTTP %d): %s — the per-entity MCP tools re-dispatch into the same ungated CRUD pipeline, so an unprivileged session renames the shared catalog through the agent surface too", code, body)
	}

	// The shared row must be unchanged regardless of the envelope shape.
	code, body = e2eDo(t, pleb, "GET", base+"/api/plans/"+proID, "")
	if code != http.StatusOK {
		t.Fatalf("setup broken: re-GET plan = %d: %s", code, body)
	}
	var after struct {
		Data struct {
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &after); err != nil {
		t.Fatalf("setup broken: decode re-read plan: %v\n%s", err, body)
	}
	if after.Data.Name != "Pro" {
		t.Errorf("SECURITY: [meridian-plans-world-mutable] re-GET after MCP plans_update shows name=%q, want the untouched seeded Pro — the agent surface mutated the shared catalog", after.Data.Name)
	}
}
