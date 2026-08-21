package access

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func collectEvaluations(t *testing.T) *[]Evaluation {
	t.Helper()
	var mu sync.Mutex
	var seen []Evaluation
	SetObserver(func(e Evaluation) {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, e)
	})
	t.Cleanup(func() { SetObserver(nil) })
	return &seen
}

func TestCanObservesBothVerdicts(t *testing.T) {
	// A denial is coverage. The failure this feeds, GOFASTR1102, is
	// about a permission check that is never *reached*, so recording only
	// the grants would report a correctly-tested rejection as untested.
	seen := collectEvaluations(t)

	policy := NewRolePolicy()
	policy.Grant("editor", "posts:write")
	ctx := WithRoles(WithPolicy(context.Background(), policy), []string{"editor"})

	if !Can(ctx, "posts:write") {
		t.Fatal("granted permission was denied")
	}
	if Can(ctx, "posts:delete") {
		t.Fatal("ungranted permission was allowed")
	}

	got := map[Permission]bool{}
	for _, e := range *seen {
		got[e.Permission] = e.Granted
		if e.Path != "can" {
			t.Errorf("%s: path = %q, want \"can\"", e.Permission, e.Path)
		}
	}
	if len(got) != 2 || !got["posts:write"] || got["posts:delete"] {
		t.Errorf("evaluations = %+v", *seen)
	}
}

func TestCanObservesTheUnwiredRequest(t *testing.T) {
	// No policy in context is the secure-by-default false. It is still an
	// evaluation that happened, and recording it is what tells you the
	// check is reachable at all.
	seen := collectEvaluations(t)
	if Can(context.Background(), "posts:read") {
		t.Fatal("an unwired context granted a permission")
	}
	if len(*seen) != 1 || (*seen)[0].Granted {
		t.Errorf("evaluations = %+v", *seen)
	}
}

func TestRequirePermissionObservesItsCheck(t *testing.T) {
	// RequirePermission consults the policy directly rather than calling
	// Can, so it needs its own observation or the middleware path records
	// nothing.
	seen := collectEvaluations(t)

	policy := NewRolePolicy()
	policy.Grant("admin", "users:delete")

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	handler := RequirePermission("users:delete")(next)

	req := httptest.NewRequest(http.MethodDelete, "/users/1", nil)
	req = req.WithContext(WithRoles(WithPolicy(req.Context(), policy), []string{"admin"}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("granted request got %d", rec.Code)
	}

	denied := httptest.NewRequest(http.MethodDelete, "/users/1", nil)
	denied = denied.WithContext(WithRoles(WithPolicy(denied.Context(), policy), []string{"viewer"}))
	deniedRec := httptest.NewRecorder()
	handler.ServeHTTP(deniedRec, denied)
	if deniedRec.Code != http.StatusForbidden {
		t.Fatalf("denied request got %d, want 403", deniedRec.Code)
	}

	if len(*seen) != 2 {
		t.Fatalf("evaluations = %+v, want one per request", *seen)
	}
	for _, e := range *seen {
		if e.Path != "require-permission" {
			t.Errorf("path = %q", e.Path)
		}
		if e.Permission != "users:delete" {
			t.Errorf("permission = %q", e.Permission)
		}
	}
	if !(*seen)[0].Granted || (*seen)[1].Granted {
		t.Errorf("verdicts = %+v", *seen)
	}
}

func TestClearedObserverIsNotCalled(t *testing.T) {
	called := false
	SetObserver(func(Evaluation) { called = true })
	SetObserver(nil)
	Can(context.Background(), "posts:read")
	if called {
		t.Error("observer fired after being cleared")
	}
}

func TestCanIsConcurrencySafeWithAnObserver(t *testing.T) {
	var mu sync.Mutex
	count := 0
	SetObserver(func(Evaluation) {
		mu.Lock()
		count++
		mu.Unlock()
	})
	t.Cleanup(func() { SetObserver(nil) })

	policy := NewRolePolicy()
	policy.Grant("editor", "posts:write")
	ctx := WithRoles(WithPolicy(context.Background(), policy), []string{"editor"})

	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			Can(ctx, "posts:write")
		})
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if count != 32 {
		t.Errorf("observed %d evaluations, want 32", count)
	}
}
