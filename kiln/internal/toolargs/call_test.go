package toolargs

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/kiln/protocol"
)

// The dispatch table and the descriptor list name the same tools: a tool
// the model is told about always dispatches, and a dispatchable tool is
// always described.
func TestCallTableMatchesDescriptors(t *testing.T) {
	var described []string
	for _, d := range (*protocol.Tools)(nil).List() {
		described = append(described, d.Name)
	}
	var dispatched []string
	for name := range tools {
		dispatched = append(dispatched, name)
	}
	slices.Sort(dispatched)
	if !slices.Equal(described, dispatched) {
		t.Errorf("descriptors %v != dispatch table %v", described, dispatched)
	}
}

func TestCallUnknownTool(t *testing.T) {
	_, err := Call(context.Background(), nil, "no_such_tool", []byte(`{}`))
	if !errors.Is(err, ErrUnknownTool) {
		t.Fatalf("err = %v, want ErrUnknownTool", err)
	}
	if got, want := err.Error(), `unknown tool "no_such_tool"`; got != want {
		t.Errorf("err = %q, want %q", got, want)
	}
}

// A decode failure comes back as the error and the tool never runs: a nil
// *Tools would panic if it did.
func TestCallDecodeFailureSkipsTool(t *testing.T) {
	for _, body := range []string{``, `[]`, `{"Path":"/a","path":"/b"}`} {
		if _, err := Call(context.Background(), nil, "delete_page", []byte(body)); err == nil {
			t.Errorf("delete_page %q: want a decode error", body)
		}
	}
}
