package handler

import (
	"fmt"
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

// mustReject asserts Bind refuses body — the strict-key contract's whole
// point. reason carries the per-vector smuggling rationale verbatim, so a
// failure still names the attack it guards against.
func mustReject(t *testing.T, body, reason string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	var dst strictKeysReq
	if err := Bind(req, &dst); err == nil {
		t.Fatalf("%s", reason)
	}
}

func TestBind_RejectsDuplicateKeys(t *testing.T) {
	mustReject(t, `{"name":"alice","name":"bob"}`,
		`Bind accepted duplicate key "name"; last-key-wins lets validation see one value and the handler another`)
}

func TestBind_RejectsCaseFoldedKeys(t *testing.T) {
	for _, key := range []string{"Name", "NAME", "nAmE", "User_ID", "USER_ID"} {
		t.Run(key, func(t *testing.T) {
			mustReject(t, `{"`+key+`":"alice"}`,
				fmt.Sprintf("Bind accepted case-folded key %q; stdlib's case-insensitive match lets a body smuggle into validated fields", key))
		})
	}
}

func TestBind_RejectsUnknownFields(t *testing.T) {
	for _, key := range []string{"role", "is_admin", "tenant_id", "permissions", "api_key"} {
		t.Run(key, func(t *testing.T) {
			mustReject(t, `{"name":"alice","`+key+`":"x"}`,
				fmt.Sprintf("Bind silently ignored unknown field %q; mass-assignment vector if any downstream handler picks the body up as map[string]any", key))
		})
	}
}

// Tag json:"-" must stay off the allow-list: a body with "IsAdmin":true
// must not bind to a field the author explicitly excluded.
func TestBind_RejectsJsonDashTaggedFields(t *testing.T) {
	mustReject(t, `{"name":"alice","IsAdmin":true}`,
		`Bind accepted field tagged json:"-"; that tag is the canonical opt-out for sensitive props`)
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

// Property: duplicate detection compares DECODED key bytes, so a raw key
// and its \u-escaped spelling of the same name still count as duplicates.
// Stdlib json alone silently takes the last one — the escape is the
// smuggling shape that survives naive byte-comparison.
func TestBind_RejectsEscapedDuplicateKeys(t *testing.T) {
	for _, body := range []string{
		`{"name":"alice","n\u0061me":"bob"}`,
		`{"n\u0061me":"bob","name":"alice"}`,
	} {
		t.Run(body, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			var dst strictKeysReq
			err := Bind(req, &dst)
			if err == nil {
				t.Fatalf("escaped duplicate key accepted; last-wins smuggled name=%q", dst.Name)
			}
		})
	}
}

// A *map destination refuses duplicate and case-folded keys, as
// UnmarshalStrict does; unknown keys are a map's to keep.
func TestBind_MapDstRefusesDuplicateKeys(t *testing.T) {
	for _, body := range []string{`{"name":"a","name":"c"}`, `{"name":"a","Name":"b"}`} {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		var dst map[string]any
		if err := Bind(req, &dst); err == nil {
			t.Fatalf("map destination accepted %s, last-wins %v", body, dst)
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"unknown":1}`))
	req.Header.Set("Content-Type", "application/json")
	var dst map[string]any
	if err := Bind(req, &dst); err != nil || dst["unknown"] == nil {
		t.Fatalf("map destination refused an ordinary key: %v", err)
	}
}

// Duplicate and case-folded keys nested below the top level are refused,
// as ToolAdapter refuses them for the same Handler's MCP twin.
func TestBind_NestedDupKeysRefused(t *testing.T) {
	type inner struct {
		Role string `json:"role"`
	}
	var dst struct {
		User inner `json:"user"`
	}
	for _, body := range []string{`{"user":{"role":"member","role":"admin"}}`, `{"user":{"role":"member","Role":"admin"}}`} {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if err := Bind(req, &dst); err == nil {
			t.Fatalf("nested duplicate accepted in %s, role=%q", body, dst.User.Role)
		}
	}
}
