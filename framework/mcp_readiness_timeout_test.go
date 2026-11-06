package framework

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestReadinessToolUsesProbeDeadline(t *testing.T) {
	for _, timeout := range []time.Duration{15 * time.Millisecond, 35 * time.Millisecond} {
		t.Run(timeout.String(), func(t *testing.T) {
			application := NewApp(WithMCPIntrospection(), WithReadinessTimeout(timeout))
			application.RegisterReadiness("slow", func(ctx context.Context) error {
				if _, bounded := ctx.Deadline(); !bounded {
					return nil
				}
				<-ctx.Done()
				return ctx.Err()
			})
			if err := application.InitPlugins(); err != nil {
				t.Fatal(err)
			}
			_, readiness := application.healthHandlers()
			response := httptest.NewRecorder()
			readiness(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
			if response.Code != http.StatusServiceUnavailable {
				t.Fatalf("probe did not time out: %d", response.Code)
			}
			result, err := application.MCP.CallTool(context.Background(), "app_readiness", nil)
			if err != nil {
				t.Fatal(err)
			}
			if result.(map[string]any)["ready"] != false {
				t.Fatalf("tool disagrees with timed-out probe: %#v", result)
			}
		})
	}
}

func TestReadinessToolKeepsCallerDeadline(t *testing.T) {
	application := NewApp(WithMCPIntrospection(), WithReadinessTimeout(time.Second))
	parent, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	want, _ := parent.Deadline()
	application.RegisterReadiness("deadline", func(ctx context.Context) error {
		if got, ok := ctx.Deadline(); !ok || !got.Equal(want) {
			t.Errorf("caller deadline changed: %v, %v; want %v", got, ok, want)
		}
		return nil
	})
	if err := application.InitPlugins(); err != nil {
		t.Fatal(err)
	}
	if _, err := application.MCP.CallTool(parent, "app_readiness", nil); err != nil {
		t.Fatal(err)
	}
}
