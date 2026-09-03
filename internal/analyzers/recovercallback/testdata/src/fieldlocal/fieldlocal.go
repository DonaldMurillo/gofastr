// Package fieldlocal pins the local-copy leg (review finding R2): a
// func field copied to a local and called through the local — the
// nil-check-and-call spelling any careful author writes — is the same
// registry callback as the direct field call one line earlier.
package fieldlocal

import (
	"bufio"
	"context"
)

type Tool struct {
	Gate func(ctx context.Context) error
}

type S struct{ tools map[string]Tool }

func (s *S) Serve(in *bufio.Reader) {
	scanner := bufio.NewScanner(in)
	for scanner.Scan() {
		s.Handle(scanner.Bytes())
	}
	_ = scanner.Err()
}

func (s *S) Handle(line []byte) {
	t, ok := s.tools[string(line)]
	if !ok {
		return
	}
	s.Call(t)
	s.callDirect(t)
}

func (s *S) Call(t Tool) {
	gate := t.Gate
	if gate != nil {
		_ = gate(context.Background()) // want `recovercallback: gate is invoked with no recover in scope`
	}
}

// callDirect is the flagship spelling beside it: the field itself.
func (s *S) callDirect(t Tool) {
	_ = t.Gate(context.Background()) // want `recovercallback: t\.Gate is invoked with no recover in scope`
}

// infraLocal: a printf-shaped field copied to a local is still
// plumbing, not an app callback. Quiet.
type L struct {
	logf func(format string, args ...any)
}

func (l *L) run(reqs <-chan string) {
	for r := range reqs {
		logf := l.logf
		logf("seen %s", r)
	}
}
