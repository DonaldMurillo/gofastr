package framework

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/a2a"
)

// TestA2A_ShutdownEndsDetachedRuns: a returnImmediately run outlives its
// request, so App.Shutdown is the only thing that can stop it. Shutdown
// cancels the run and does not return before the handler has.
func TestA2A_ShutdownEndsDetachedRuns(t *testing.T) {
	started, returned := make(chan struct{}), make(chan struct{})
	hold := a2a.Skill{
		ID: "hold", Name: "Hold", Description: "Runs until cancelled.",
		Handler: func(ctx context.Context, _ a2a.TaskContext) error {
			close(started)
			<-ctx.Done()
			close(returned)
			return ctx.Err()
		},
	}
	e := newA2ATestEnv(t, A2AConfig{Skills: []a2a.Skill{hold}, DisableEntitySkills: true})
	status, body := e.a2aRequest(t, "tok-alice", `{"jsonrpc":"2.0","id":1,"method":"SendMessage","params":{`+
		`"message":{"messageId":"m1","role":"ROLE_USER","parts":[{"text":"go"}],"metadata":{"skill":"hold"}},`+
		`"configuration":{"returnImmediately":true}}}`)
	if status != http.StatusOK {
		t.Fatalf("SendMessage: %d %s", status, body)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatalf("the skill never started: %s", body)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = e.app.Shutdown(ctx)
	select {
	case <-returned:
	default:
		t.Fatal("App.Shutdown returned while the A2A run was still running")
	}
}
