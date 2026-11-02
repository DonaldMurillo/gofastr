// Package a2aish reconstructs core/a2a before 6fce08fe6, the two
// goroutines this rule missed. The run goroutine is a literal: before
// the literal nodes linked their own bodies, its call to s.finalize was
// attributed to startRun, which no dispatch path reaches, so the Store
// writes under finalize never went hot. The push drain ran the host's
// PushOptions.Client (its Transport, its CheckRedirect) through a
// concrete *http.Client method, which the rule dropped as plumbing.
package a2aish

import (
	"context"
	"log/slog"
	"net/http"
)

type Task struct{ ID string }

// Store is the host-supplied extension point (a2a.Store).
type Store interface {
	UpdateTask(ctx context.Context, t *Task) error
	ListPushConfigs(ctx context.Context, owner, id string) ([]string, error)
}

type PushOptions struct {
	Client *http.Client
}

type Server struct {
	store Store
}

// startRun is the pre-fix shape: finalize's Store calls run on the
// goroutine literal with no recover anywhere on it.
func (s *Server) startRun(ctx context.Context, t *Task) {
	go func() {
		s.invoke(ctx, t)
		s.finalize(ctx, t)
	}()
}

func (s *Server) invoke(ctx context.Context, t *Task) {}

func (s *Server) finalize(ctx context.Context, t *Task) {
	_ = s.store.UpdateTask(ctx, t) // want `recovercallback: s\.store\.UpdateTask is invoked with no recover in scope`
	s.publish(ctx, t)
}

func (s *Server) publish(ctx context.Context, t *Task) {
	_, _ = s.store.ListPushConfigs(ctx, "o", t.ID) // want `recovercallback: s\.store\.ListPushConfigs is invoked with no recover in scope`
}

// Fixed is the 6fce08fe6 spelling: the goroutine literal defers a
// recover, which catches every panic finalize and its callees raise, so
// the flood stops at the literal and fixedFinalize stays quiet.
type Fixed struct {
	store Store
}

func (s *Fixed) startRun(ctx context.Context, t *Task) {
	go func() {
		defer func() {
			if p := recover(); p != nil {
				_ = p
			}
		}()
		s.fixedFinalize(ctx, t)
	}()
}

func (s *Fixed) fixedFinalize(ctx context.Context, t *Task) {
	_ = s.store.UpdateTask(ctx, t) // quiet: the launching literal recovers
}

// NestedOnly: a recover in a goroutine the literal launches runs on that
// goroutine's own stack and does not cover the launching literal's
// synchronous callees. Fires.
type NestedOnly struct {
	store Store
}

func (s *NestedOnly) startRun(ctx context.Context, t *Task) {
	go func() {
		go func() {
			defer func() { _ = recover() }()
		}()
		s.nestedFinalize(ctx, t)
	}()
}

func (s *NestedOnly) nestedFinalize(ctx context.Context, t *Task) {
	_ = s.store.UpdateTask(ctx, t) // want `recovercallback: s\.store\.UpdateTask is invoked with no recover in scope`
}

// pusher is the push drain before 6fce08fe6: the host's client runs on
// the drain goroutine bare.
type pusher struct {
	client *http.Client
}

func newPusher(opts PushOptions) *pusher {
	var client *http.Client
	if opts.Client != nil {
		client = guardedClient(opts.Client)
	} else {
		client = &http.Client{}
	}
	return &pusher{client: client}
}

func guardedClient(c *http.Client) *http.Client {
	cc := *c
	return &cc
}

func (p *pusher) enqueue(req *http.Request) {
	go p.drain(req)
}

func (p *pusher) drain(req *http.Request) {
	p.post(req)
	p.postGuarded(req)
}

func (p *pusher) post(req *http.Request) {
	resp, err := p.client.Do(req) // want `recovercallback: p\.client\.Do is invoked with no recover in scope`
	if err == nil {
		_ = resp.Body.Close()
	}
}

// postGuarded is the fix spelling: the delivery runs under its own
// recover.
func (p *pusher) postGuarded(req *http.Request) {
	defer func() { _ = recover() }()
	resp, err := p.client.Do(req) // quiet: recovered on this frame
	if err == nil {
		_ = resp.Body.Close()
	}
}

// ownClient is the near miss: a client the package builds from a
// literal (directly or through a local) runs net/http's own transport,
// not host code, however hot the path.
type ownClient struct {
	client *http.Client
	other  *http.Client
}

func newOwnClient() *ownClient {
	c := &http.Client{}
	return &ownClient{client: c, other: http.DefaultClient}
}

func (o *ownClient) loop(reqs <-chan *http.Request) {
	for req := range reqs {
		resp, err := o.client.Do(req) // quiet: package-built client
		if err == nil {
			_ = resp.Body.Close()
		}
		resp, err = o.other.Get("http://x") // quiet: http.DefaultClient
		if err == nil {
			_ = resp.Body.Close()
		}
	}
}

// Exported: an exported *http.Client field is the host's to set. A
// goroutine the package launches sending through it fires.
type Exported struct {
	HTTP *http.Client
}

func (e *Exported) Start(reqs <-chan *http.Request) {
	go e.loop(reqs)
}

func (e *Exported) loop(reqs <-chan *http.Request) {
	for req := range reqs {
		resp, err := e.HTTP.Do(req) // want `recovercallback: e\.HTTP\.Do is invoked with no recover in scope`
		if err == nil {
			_ = resp.Body.Close()
		}
	}
}

// Watch is the near miss the generated API client hit: a read loop the
// HOST calls synchronously (watchSSE scanning a stream). The host's
// Transport runs on the host's own goroutine, so a panic there unwinds
// into the host's call, not a package goroutine. Quiet.
func (e *Exported) Watch(req *http.Request, fn func(string)) error {
	resp, err := e.HTTP.Do(req) // quiet: no goroutine of this package reaches it
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	buf := make([]byte, 1)
	for {
		if _, err := resp.Body.Read(buf); err != nil {
			return nil
		}
		fn(string(buf))
	}
}

// toolRunner is a test seam: its methods are unexported, so only this
// package can implement it, and the edge flood reaches the
// implementations directly (battery/service's exec seam). Quiet.
type toolRunner interface {
	output(name string) ([]byte, error)
}

type service struct {
	tool toolRunner
}

func (s *service) Start() {
	go func() {
		_, _ = s.tool.output("ctl") // quiet: package-local seam
	}()
}

// manager holds the owning App's logger as a getter: logging plumbing,
// not a callback, however hot the path.
type manager struct {
	logger func() *slog.Logger
}

func (m *manager) Start() {
	go func() {
		if m.logger != nil {
			m.logger().Info("started") // quiet: logger accessor
		}
	}()
}

// syncLiteral: a synchronous literal inside a hot function carries its
// owner's hotness to the named calls in its body.
type syncLiteral struct {
	store Store
}

func (s *syncLiteral) loop(ctx context.Context, in <-chan *Task) {
	for t := range in {
		func() {
			s.write(ctx, t)
		}()
	}
}

func (s *syncLiteral) write(ctx context.Context, t *Task) {
	_ = s.store.UpdateTask(ctx, t) // want `recovercallback: s\.store\.UpdateTask is invoked with no recover in scope`
}
