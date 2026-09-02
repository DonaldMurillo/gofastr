// Package toolgate mirrors core/mcp's tool-call path, reduced to the
// shape: a stdio loop dispatching into a tool registry whose entries
// carry a per-caller Gate field. badCallTool is the pre-fix callTool;
// the fixed spelling routes the gate through a recovered helper.
package toolgate

import (
	"bufio"
	"context"
	"errors"
)

type Gate func(ctx context.Context) error

type Tool struct {
	Name string
	Gate Gate
}

type Server struct {
	tools map[string]Tool
}

// serveStdio is the seed: for scanner.Scan() is a read loop with no
// net/http net under it.
func (s *Server) serveStdio(in *bufio.Reader) error {
	scanner := bufio.NewScanner(in)
	for scanner.Scan() {
		s.handleRequest(scanner.Bytes())
	}
	return scanner.Err()
}

func (s *Server) handleRequest(line []byte) {
	name := string(line)
	if t, ok := s.tools[name]; ok {
		_, _ = s.callTool(context.Background(), t)
	}
}

// callTool is the pre-fix shape: the gate field is evaluated directly,
// three frames below the read loop, with no recover.
func (s *Server) callTool(ctx context.Context, t Tool) (any, error) {
	if t.Gate != nil {
		if err := t.Gate(ctx); err != nil { // want `recovercallback: t\.Gate is invoked with no recover in scope`
			return nil, errors.New("tool unavailable")
		}
	}
	return nil, nil
}

// callToolFixed is the b79942f7 spelling: the gate runs inside
// checkToolGate, whose deferred recover turns a panicking gate into a
// refusal.
func (s *Server) callToolFixed(ctx context.Context, t Tool) (any, error) {
	if err := s.checkToolGate(ctx, t); err != nil {
		return nil, err
	}
	return nil, nil
}

func (s *Server) checkToolGate(ctx context.Context, t Tool) (err error) {
	if t.Gate == nil {
		return nil
	}
	defer func() {
		if rec := recover(); rec != nil {
			err = errors.New("internal tool error")
		}
	}()
	return t.Gate(ctx)
}

// invokeHandler stays quiet even though it calls a func field: it has
// its own recover, exactly like the repo's real invokeHandler.
func (s *Server) invokeHandler(ctx context.Context, t Tool) (out any, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = errors.New("internal tool error")
		}
	}()
	return nil, nil
}

// plainCaller is quiet: not reachable from any loop or goroutine.
func (s *Server) plainCaller(ctx context.Context, t Tool) error {
	if t.Gate != nil {
		return t.Gate(ctx)
	}
	return nil
}
