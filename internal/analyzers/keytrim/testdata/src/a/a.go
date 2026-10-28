// Package a holds the keytrim fixtures, reduced from the 2026-10-27 bug
// bash sites: framework/entityui bulk.go dedupeIDs (U9) and
// battery/admin rbac_admin.go's roles handler (A42).
package a

import (
	"net/http"
	"net/url"
	"os"
	"strings"
)

// dedupeIDs is bulk.go:455: the trimmed id is the one later deleted,
// so selecting " x " deletes "x".
func dedupeIDs(ids []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, id := range ids {
		id = strings.TrimSpace(id) // want `keytrim: strings\.TrimSpace rewrites the identity value id`
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// saveRoles is rbac_admin.go:549: the form names user " u1 ", the
// update binds "u1".
func saveRoles(vals url.Values, update func(string) error) error {
	userID := strings.TrimSpace(vals.Get("user_id")) // want `keytrim: strings\.TrimSpace rewrites the identity value vals\.Get\("user_id"\)`
	return update(userID)
}

// formValue: the request accessor spelling.
func formValue(r *http.Request, find func(string)) {
	find(strings.TrimSpace(r.FormValue("tenantID"))) // want `keytrim: strings\.TrimSpace rewrites the identity value r\.FormValue\("tenantID"\)`
}

// selectorField: a struct field named like an id.
type spec struct{ OwnerID string }

func ownerKey(s spec) string {
	return strings.TrimSpace(s.OwnerID) // want `keytrim: strings\.TrimSpace rewrites the identity value s\.OwnerID`
}

// emptinessOnly: a presence check never becomes the key.
func emptinessOnly(s spec, userID string) bool {
	if strings.TrimSpace(s.OwnerID) == "" {
		return false
	}
	return "" != strings.TrimSpace(userID)
}

// lenCheck: the same, spelled with len.
func lenCheck(id string) bool {
	return len(strings.TrimSpace(id)) == 0
}

// fileContents: a secret or id read from a file loses its trailing
// newline on purpose; the operand is file bytes, not an identity value.
func fileContents(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(string(b))
	return id, nil
}

// notIdentity: names, queries and emails are typed by people, and
// trimming them is the point.
func notIdentity(vals url.Values, name, email string) (string, string, string) {
	return strings.TrimSpace(name), strings.TrimSpace(email), strings.TrimSpace(vals.Get("q"))
}

// nearMiss names: "valid" and "idle" contain "id" but name no identity.
func nearMiss(valid, idle, paid string) string {
	return strings.TrimSpace(valid) + strings.TrimSpace(idle) + strings.TrimSpace(paid)
}

// otherGet: a Get on a type that is not a request read is not judged
// by its literal key.
type registry map[string]string

func (r registry) Get(k string) string { return r[k] }

func otherGet(r registry) string {
	return strings.TrimSpace(r.Get("id"))
}

// dynamicKey: a key that is not a literal is not this rule's to judge.
func dynamicKey(vals url.Values, key string) string {
	return strings.TrimSpace(vals.Get(key))
}
