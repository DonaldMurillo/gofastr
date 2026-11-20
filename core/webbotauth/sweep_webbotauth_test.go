package webbotauth

import (
	"context"
	"testing"
	"time"
)

func TestSweepCanceledRefreshSetsNoBackoff(t *testing.T) {
	md := newMutableDirectory(t, `{"keys":[{"kty":"OKP","crv":"Ed25519","x":"JrQLj5P_89iXES9-vFgrIy29clF9CC_oPPsw3c5D0bs","use":"sig"}]}`)
	res := md.resolver(t)
	now := time.Now()
	res.now = func() time.Time { return now }
	ref := &agentRef{identifier: "https://leaver.test/x", fetchURL: md.srv.srv.URL, typ: discoveryJWKS}
	if _, err := res.resolve(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
	md.fail.Store(true)
	now = now.Add(2 * time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _ = res.resolve(ctx, ref)
	before := md.hits.Load()
	if _, err := res.resolve(context.Background(), ref); err != nil {
		t.Fatalf("stale entry not served: %v", err)
	}
	if md.hits.Load() != before+1 {
		t.Fatal("a caller that left set a refetch backoff")
	}
}
