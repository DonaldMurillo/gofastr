package main

// Pins: uniqueness on an owner-scoped entity is per owner. customers.email
// and invoices.number are unique within one account's rows, never across
// accounts.
//
// Finding (2026-10-04, pre-fix): both columns carried a global UNIQUE while
// the rows were owner-scoped (owner_field: user_id). User A POST
// /api/customers {email: shared-dup@corp.test} → 201; user B, same body →
// 409 while B's own scoped list was empty. One account's row blocked every
// other account's create, and the 409 told B that the email exists in
// someone else's private data.

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/dotenv"
	"github.com/DonaldMurillo/gofastr/framework"
)

// ownerUniqueCreate POSTs body to path and fails unless the create
// returned 201. Returns the new row's id.
func ownerUniqueCreate(t *testing.T, who string, client *http.Client, base, path, body string) string {
	t.Helper()
	code, resp := e2eDo(t, client, "POST", base+path, body)
	if code != http.StatusCreated {
		t.Errorf("SECURITY: [meridian-owner-unique-global] %s POST %s = %d: %s — uniqueness on an owner-scoped entity leaks across accounts (want 201: another account's row must not block this one)", who, path, code, resp)
		return ""
	}
	return e2eExtractID(resp)
}

// ownerUniqueCount returns how many rows the caller's scoped list holds.
func ownerUniqueCount(t *testing.T, client *http.Client, base, path string) int {
	t.Helper()
	code, body := e2eDo(t, client, "GET", base+path, "")
	if code != http.StatusOK {
		t.Fatalf("setup broken: GET %s = %d: %s", path, code, body)
	}
	var list struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &list); err != nil {
		t.Fatalf("setup broken: decode %s: %v\n%s", path, err, body)
	}
	return len(list.Data)
}

func TestOwnersShareUniqueValues(t *testing.T) {
	if testing.Short() {
		t.Skip("builds + boots the binary")
	}
	_ = dotenv.LoadAndApply(framework.DefaultDotEnvPaths()...)
	base := e2eBootApp(t)

	alice := plansGateUser(t, base, "alice@ownerunique.example")
	bob := plansGateUser(t, base, "bob@ownerunique.example")

	const customer = `{"name":"Shared Dup","email":"shared-dup@corp.test"}`
	aliceCust := ownerUniqueCreate(t, "alice", alice, base, "/api/customers", customer)
	bobCust := ownerUniqueCreate(t, "bob", bob, base, "/api/customers", customer)

	// Per-owner uniqueness still holds: a second row with the same email
	// in ONE account is refused.
	if code, body := e2eDo(t, alice, "POST", base+"/api/customers", customer); code != http.StatusConflict {
		t.Errorf("alice's duplicate email in her own account = %d: %s, want 409 (uniqueness must hold within an owner)", code, body)
	}

	if aliceCust == "" || bobCust == "" {
		return
	}
	for who, c := range map[string]*http.Client{"alice": alice, "bob": bob} {
		if n := ownerUniqueCount(t, c, base, "/api/customers"); n != 1 {
			t.Errorf("%s sees %d customers, want exactly her own 1", who, n)
		}
	}

	invoice := func(customerID string) string {
		return `{"customer_id":"` + customerID + `","number":"INV-SHARED-1","amount":"10"}`
	}
	ownerUniqueCreate(t, "alice", alice, base, "/api/invoices", invoice(aliceCust))
	ownerUniqueCreate(t, "bob", bob, base, "/api/invoices", invoice(bobCust))
	if code, body := e2eDo(t, bob, "POST", base+"/api/invoices", invoice(bobCust)); code != http.StatusConflict {
		t.Errorf("bob's duplicate invoice number in his own account = %d: %s, want 409", code, body)
	}
}
