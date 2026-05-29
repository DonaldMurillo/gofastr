package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Strict top-level key validation against the destination's json tags
// is the only thing standing between stdlib's permissive JSON decoder
// and mass-assignment / last-key-wins / case-fold smuggling. These
// three tests pin down the contract.

type strictKeysReq struct {
	Name    string `json:"name"`
	UserID  string `json:"user_id"`
	IsAdmin bool   `json:"-"`
}

func TestBind_RejectsDuplicateKeys(t *testing.T) {
	body := `{"name":"alice","name":"bob"}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	var dst strictKeysReq
	if err := Bind(req, &dst); err == nil {
		t.Fatalf("Bind accepted duplicate key %q; last-key-wins lets validation see one value and the handler another", "name")
	}
}

func TestBind_RejectsCaseFoldedKeys(t *testing.T) {
	for _, key := range []string{"Name", "NAME", "nAmE", "User_ID", "USER_ID"} {
		t.Run(key, func(t *testing.T) {
			body := `{"` + key + `":"alice"}`
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")

			var dst strictKeysReq
			if err := Bind(req, &dst); err == nil {
				t.Fatalf("Bind accepted case-folded key %q; stdlib's case-insensitive match lets a body smuggle into validated fields", key)
			}
		})
	}
}

func TestBind_RejectsUnknownFields(t *testing.T) {
	for _, key := range []string{"role", "is_admin", "tenant_id", "permissions", "api_key"} {
		t.Run(key, func(t *testing.T) {
			body := `{"name":"alice","` + key + `":"x"}`
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")

			var dst strictKeysReq
			if err := Bind(req, &dst); err == nil {
				t.Fatalf("Bind silently ignored unknown field %q; mass-assignment vector if any downstream handler picks the body up as map[string]any", key)
			}
		})
	}
}

// Tag json:"-" must stay off the allow-list — a body with "IsAdmin":true
// must not bind to a field the author explicitly excluded.
func TestBind_RejectsJsonDashTaggedFields(t *testing.T) {
	body := `{"name":"alice","IsAdmin":true}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	var dst strictKeysReq
	if err := Bind(req, &dst); err == nil {
		t.Fatalf("Bind accepted field tagged json:\"-\"; that tag is the canonical opt-out for sensitive props")
	}
}

type embeddedCommon struct {
	Name string `json:"name"`
}

type embeddedReq struct {
	embeddedCommon
	Age int `json:"age"`
}

// Strict-key validation must not over-reject: encoding/json promotes JSON
// keys from anonymous embedded structs, so the allow-list has to recurse
// into them. Otherwise every endpoint whose bind struct embeds another
// struct 400s on fully-valid bodies (a functional DoS), while a still-
// unknown key must keep being rejected.
func TestBind_AcceptsEmbeddedStructKeys(t *testing.T) {
	t.Run("promoted key accepted", func(t *testing.T) {
		body := `{"name":"alice","age":30}`
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		var dst embeddedReq
		if err := Bind(req, &dst); err != nil {
			t.Fatalf("Bind rejected promoted embedded key: %v", err)
		}
		if dst.Name != "alice" || dst.Age != 30 {
			t.Fatalf("Bind dropped values: %+v", dst)
		}
	})

	t.Run("still rejects unknown key", func(t *testing.T) {
		body := `{"name":"alice","age":30,"is_admin":true}`
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		var dst embeddedReq
		if err := Bind(req, &dst); err == nil {
			t.Fatalf("Bind accepted unknown field alongside embedded keys; strict-key protection lost")
		}
	})
}
