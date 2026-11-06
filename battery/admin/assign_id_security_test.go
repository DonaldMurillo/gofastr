package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"reflect"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/auth"
)

type assignLookupStore struct {
	auth.UserStore
	lookups int
}

func (store *assignLookupStore) FindByID(ctx context.Context, id string) (auth.User, error) {
	store.lookups++
	if !storableText(id) {
		return nil, errors.New("unstorable identifier reached storage")
	}
	return store.UserStore.FindByID(ctx, id)
}

func TestAssignRefusesUnstorableID(t *testing.T) {
	world := newRBACEnv(t, Config{})
	store := &assignLookupStore{UserStore: world.mgr.UserStore()}
	world.mgr.SetUserStore(store)
	before := world.rolesOf(world.editor.GetID())
	for _, invalid := range []string{"id\x00tail", "id\xff", "id\xe2\x82"} {
		lookups := store.lookups
		response := post(world.as(theAdmin), "/admin/rbac/_assign", url.Values{"user_id": {invalid}, "roles": {"support"}})
		if got := resultOf(t, response); got != "bad-input" {
			t.Errorf("unstorable id %q returned %q", invalid, got)
		}
		if invalid == "id\x00tail" {
			response = rpc(world.as(theAdmin), "/admin/rbac/_assign", map[string]any{"user_id": invalid, "roles": "support"})
			var refusal struct {
				Fields map[string]string `json:"fields"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &refusal); err != nil || response.Code != http.StatusBadRequest || refusal.Fields["user_id"] == "" {
				t.Errorf("RPC lacks keyed user_id 400: %d %s (%v)", response.Code, response.Body, err)
			}
		}
		if store.lookups != lookups {
			t.Error("unstorable identifier reached FindByID")
		}
	}
	if !reflect.DeepEqual(before, world.rolesOf(world.editor.GetID())) || len(world.auditOps("access")) != 0 {
		t.Error("invalid identifier changed roles or emitted an audit")
	}
}
