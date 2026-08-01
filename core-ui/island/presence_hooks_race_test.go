package island

import (
	"context"
	"sync"
	"testing"
)

// The presence hooks (OnPresenceChange, AuthorizeTopic) are set-once globals
// read on every roster change / SSE connect. Driving concurrent installs via
// the setters alongside the read paths must be race-free (the hooks are backed
// by atomic.Pointer). Under the old plain-field design this was a data race.

func TestPresenceHooksConcurrentSetAndRead(t *testing.T) {
	m := NewManager()
	ctx := context.Background()
	allow := func(context.Context, string) bool { return true }
	deny := func(context.Context, string) bool { return false }
	cb := func(string) {}

	var wg sync.WaitGroup
	for range 200 {
		wg.Add(4)
		go func() { defer wg.Done(); m.SetAuthorizeTopic(allow) }()
		go func() { defer wg.Done(); m.SetAuthorizeTopic(deny) }()
		go func() { defer wg.Done(); m.SetOnPresenceChange(cb) }()
		go func() { defer wg.Done(); _ = m.filterAuthorizedTopics(ctx, []string{"a", "b"}) }()
	}
	wg.Wait()
}
