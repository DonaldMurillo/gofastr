// Package helperguard pins the named-guard leg (review finding R5):
// a deferred call to a package-local function or method whose body
// recovers is as much a recover as the inline literal — recover works
// when called directly by the deferred function, and the guard IS the
// deferred function. Deferring a named guard is the standard idiom in
// codebases that centralize panic handling.
package helperguard

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
	s.CallGuarded(t)
	s.CallGuardedMethod(t)
	s.CallUnguarded(t)
}

// CallGuarded defers the named guard: quiet.
func (s *S) CallGuarded(t Tool) {
	defer guard()
	_ = t.Gate(context.Background())
}

// CallGuardedMethod defers a method guard: quiet.
func (s *S) CallGuardedMethod(t Tool) {
	defer s.guard()
	_ = t.Gate(context.Background())
}

// CallUnguarded is the control: same path, no guard anywhere.
func (s *S) CallUnguarded(t Tool) {
	_ = t.Gate(context.Background()) // want `recovercallback: t\.Gate is invoked with no recover in scope`
}

func guard() {
	if r := recover(); r != nil {
		_ = r
	}
}

func (s *S) guard() {
	if r := recover(); r != nil {
		_ = r
	}
}
