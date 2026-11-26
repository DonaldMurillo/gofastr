package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type failWriter struct{ err error }

func (w failWriter) Write([]byte) (int, error) { return 0, w.err }

// A reply ServeStdio cannot write ends the serve with that error; the
// client got nothing, so a clean nil would hide it.
func TestServeStdioReturnsWriteError(t *testing.T) {
	closed := errors.New("closed pipe")
	for name, input := range map[string]string{
		"reply":          `{"jsonrpc":"2.0","id":1,"method":"ping"}` + "\n",
		"envelope error": `{"jsonrpc":"2.0","id":1` + "\n",
		"line too long":  strings.Repeat("x", maxMCPBodyBytes+1) + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			s := NewServer()
			err := s.ServeStdio(context.Background(), strings.NewReader(input+`{"jsonrpc":"2.0","id":2,"method":"ping"}`+"\n"), failWriter{closed})
			if !errors.Is(err, closed) {
				t.Errorf("ServeStdio = %v, want the write error", err)
			}
		})
	}
}
