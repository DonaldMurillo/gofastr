package moduleproto

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"
)

// Property (2026-10-31 bug hunt, O42): the over-cap error answer a
// serving peer writes for a request past its serve cap must not block the
// read loop. It used to be written inline on the read loop, outside the
// unwritten-response bound, so a counterparty that filled the serve cap
// and stopped reading wedged the read loop: no fault was recorded, the
// supervisor never killed the child, and the slot stayed Ready.
func TestOverCapAnswerNeverWedgesReadLoop(t *testing.T) {
	inR, inW := io.Pipe()   // the counterparty writes requests here
	outR, outW := io.Pipe() // the peer's responses; nobody reads them
	t.Cleanup(func() {
		_ = outR.Close()
		_ = inW.Close()
	})
	codec, err := NewCodec(inR, outW, 0)
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	started := make(chan struct{}, 1)
	peer := NewPeer(codec, RoleHost,
		WithMaxServeInflight(1),
		WithHandler("hold", func(ctx context.Context, _ json.RawMessage) (any, error) {
			started <- struct{}{}
			select {
			case <-release:
			case <-ctx.Done():
			}
			return "ok", nil
		}),
	)
	peer.Start()
	t.Cleanup(func() { _ = peer.Close() })

	// Fill the one serve slot, then send over-cap requests without ever
	// reading an answer.
	if _, err := inW.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"hold"}` + "\n")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first request never reached its handler")
	}
	go func() {
		for i := 2; i < 10; i++ {
			line := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"hold"}`+"\n", i)
			if _, err := inW.Write([]byte(line)); err != nil {
				return
			}
		}
	}()
	select {
	case <-peer.FatalDone():
		if !errors.Is(peer.FatalError(), ErrStalled) {
			t.Fatalf("fatal = %v, want ErrStalled", peer.FatalError())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("SECURITY: [moduleproto-overcap-stall] over-cap answers nobody read wedged the read loop with no fault recorded")
	}
}
