package admin

import (
	"encoding/json"
	"net/http"
	"net/url"
	"reflect"
	"testing"
)

func TestRBACRefusesUnstorableNames(t *testing.T) {
	for _, operation := range []string{"_grant", "_permissions"} {
		for _, field := range []string{"role", "permission"} {
			for _, invalid := range []string{"prefix\x00suffix", "invalid\xff", "truncated\xc3"} {
				t.Run(operation+"/"+field+"/"+url.QueryEscape(invalid), func(t *testing.T) {
					world := newRBACEnv(t, Config{})
					before := world.policy.Snapshot()
					values := url.Values{"role": {"editor"}, "permission": {"posts:read"}}
					if operation == "_permissions" {
						values["role"] = []string{"editor", "support"}
						values["permission"] = []string{"posts:read", "queue:read"}
					}
					values[field][len(values[field])-1] = invalid
					response := post(world.as(theAdmin), "/admin/rbac/"+operation, values)
					if got := resultOf(t, response); got != "bad-input" {
						t.Errorf("unstorable %s: result %q", field, got)
					}
					if !reflect.DeepEqual(before, world.policy.Snapshot()) || len(world.auditOps("access")) != 0 {
						t.Error("refused input changed grants or emitted a successful audit")
					}
				})
			}
		}
	}
	world := newRBACEnv(t, Config{})
	response := rpc(world.as(theAdmin), "/admin/rbac/_grant", map[string]any{"role": "new\x00role", "permission": "posts:read"})
	if response.Code != http.StatusBadRequest {
		t.Errorf("RPC status %d, want 400", response.Code)
	}
	var refusal struct {
		Fields map[string]string `json:"fields"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &refusal); err != nil || refusal.Fields["role"] == "" {
		t.Errorf("RPC lacks a keyed role refusal: %s (%v)", response.Body.String(), err)
	}
	for _, invalid := range []string{"role\x00suffix", "bad\xff", "short\xc3"} {
		before := world.rolesOf(world.editor.GetID())
		response := post(world.as(theAdmin), "/admin/rbac/_assign", url.Values{"user_id": {world.editor.GetID()}, "roles": {"support", invalid}})
		if got := resultOf(t, response); got != "bad-input" || !reflect.DeepEqual(before, world.rolesOf(world.editor.GetID())) {
			t.Errorf("assignment accepted unstorable role %q: %s", invalid, got)
		}
	}
}
