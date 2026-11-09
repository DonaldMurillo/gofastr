package a2a

import (
	"net/http"
	"testing"
)

// An Owner or Tenant callback that reads request bytes the SQL store
// cannot bind (a NUL byte, invalid UTF-8) is refused at resolution:
// before those bytes reached a task INSERT or a scoped WHERE they
// answered Postgres's 22021 as a 500, while SQLite silently stored
// them. The owner is refused with the transport's 401; the tenant
// fails every method the same way, because a tenant that cannot be
// stored cannot scope a task.
func TestUnstorableOwnerAndTenantRefused(t *testing.T) {
	t.Run("owner NUL", func(t *testing.T) {
		h := newHarness(t, func(c *Config) {
			c.Owner = func(r *http.Request) (string, bool) { return "a\x00b", true }
		})
		status, e, _ := h.call("", MethodListTasks, map[string]any{})
		if status != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401 for a NUL-bearing owner", status)
		}
		if e.Error == nil || e.Error.Code != CodeUnauthenticated {
			t.Fatalf("error = %+v, want unauthenticated", e.Error)
		}
	})
	t.Run("owner invalid UTF-8", func(t *testing.T) {
		h := newHarness(t, func(c *Config) {
			c.Owner = func(r *http.Request) (string, bool) { return "a\xffb", true }
		})
		status, _, _ := h.call("", MethodListTasks, map[string]any{})
		if status != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401 for a non-UTF-8 owner", status)
		}
	})
	t.Run("tenant NUL", func(t *testing.T) {
		h := newHarness(t, func(c *Config) {
			c.Tenant = func(r *http.Request) string { return "t\x00x" }
		})
		// Every dispatch path refuses; message/send exercises the
		// prepare path, tasks/list the dispatch one.
		status, e, raw := h.call("alice", MethodSendMessage, map[string]any{
			"message": map[string]any{
				"role":  "ROLE_USER",
				"parts": []any{map[string]any{"kind": "text", "text": "hi"}},
				"metadata": map[string]any{
					"gofastr.skill": "echo",
				},
			},
		})
		if e.Error == nil || e.Error.Code != CodeUnauthenticated {
			t.Fatalf("message/send: error = %+v body=%s, want unauthenticated", e.Error, raw)
		}
		if status != http.StatusUnauthorized {
			t.Fatalf("message/send status = %d, want 401", status)
		}
		_, e, _ = h.call("alice", MethodListTasks, map[string]any{})
		if e.Error == nil || e.Error.Code != CodeUnauthenticated {
			t.Fatalf("tasks/list: error = %+v, want unauthenticated", e.Error)
		}
	})
	t.Run("storable tenant passes", func(t *testing.T) {
		h := newHarness(t, func(c *Config) {
			c.Tenant = func(r *http.Request) string { return "acme" }
		})
		task := h.send("alice", "hi")
		if task == nil {
			t.Fatal("send under a storable tenant failed")
		}
	})
}
