package appgen

import "testing"

// An acronym is one word: getUserByID is get-user-by-id, not
// get-user-by-i-d, and a GoFastr column HTTPCode is --http-code. The
// boundary rule is appspec.ToSnakeCase's: a capital after a lowercase
// letter or digit starts a word, and so does the last capital of a run
// when a lowercase letter follows it (HTTPServer is http-server).
func TestOAKebabKeepsAcronymsWhole(t *testing.T) {
	for in, want := range map[string]string{
		"getUserByID":  "get-user-by-id",
		"HTTPCode":     "http-code",
		"X-Request-ID": "x-request-id",
		"listAPIKeys":  "list-api-keys",
		"HTTPServer":   "http-server",
		"getV2Users":   "get-v2-users",
		"listPets":     "list-pets",
		"list_pets":    "list-pets",
		"ID":           "id",
		"userId":       "user-id",
		"created_at":   "created-at",
	} {
		if got := oaKebab(in); got != want || !oaValidFlag(got) {
			t.Errorf("oaKebab(%q) = %q, want %q", in, got, want)
		}
	}
}
