// Package nestedrec pins the recover boundary (review finding R3): a
// recover deferred inside a nested function literal runs on that
// literal's own stack and cannot catch a panic in the enclosing
// function's linear body — only the function's own deferred recovers
// protect it.
package nestedrec

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
}

func (s *S) Call(t Tool) {
	go func() {
		defer func() {
			_ = recover()
		}()
		s.Keepalive()
	}()
	_ = t.Gate(context.Background()) // want `recovercallback: t\.Gate is invoked with no recover in scope`
}

func (s *S) Keepalive() {}
