package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestTokenTextRejectedBeforeStore(t *testing.T) {
	_, store, _ := newTokenTestDB(t)
	for _, invalid := range []string{"a\x00b", "other\xff", "short\xe2\x82", strings.Repeat("x", 257)} {
		_, _, err := IssueToken(context.Background(), store, TokenSpec{Name: invalid, OwnerKind: OwnerKindUser, OwnerID: "alice"})
		if !errors.Is(err, ErrInvalidTokenSpec) {
			t.Errorf("name %q: got %v", invalid, err)
		}
	}
	for _, invalid := range []string{"owner\x00", "owner\xfe"} {
		_, _, err := IssueToken(context.Background(), store, TokenSpec{Name: "valid", OwnerKind: OwnerKindUser, OwnerID: invalid})
		if !errors.Is(err, ErrInvalidTokenSpec) {
			t.Errorf("owner %q: got %v", invalid, err)
		}
	}
	rows, err := store.List(context.Background(), OwnerKindUser, "alice")
	if err != nil || len(rows) != 0 {
		t.Fatalf("invalid names wrote rows: %d, %v", len(rows), err)
	}
	for _, name := range []string{strings.Repeat("x", 256), strings.Repeat("é", 128)} {
		if _, _, err := IssueToken(context.Background(), store, TokenSpec{Name: name, OwnerKind: OwnerKindUser, OwnerID: "alice"}); err != nil {
			t.Errorf("valid boundary refused: %v", err)
		}
	}
}

func TestTokenNameRefusalNamesField(t *testing.T) {
	rig := newTokenMintRig(t)
	cookie, _ := rig.login()
	for _, name := range []string{"a\x00b", strings.Repeat("z", 257)} {
		body, err := json.Marshal(map[string]string{"name": name})
		if err != nil {
			t.Fatal(err)
		}
		response := rig.do(rig.sessH, http.MethodPost, "/auth/tokens", string(body), "", cookie)
		var result struct {
			Fields map[string]string `json:"fields"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if response.Code != http.StatusBadRequest || result.Fields["name"] == "" {
			t.Errorf("got %d %s", response.Code, response.Body)
		}
	}
	var count int
	if err := rig.tokens.db.QueryRow(`SELECT COUNT(*) FROM auth_api_tokens`).Scan(&count); err != nil || count != 0 {
		t.Errorf("invalid names wrote %d tokens: %v", count, err)
	}
}
