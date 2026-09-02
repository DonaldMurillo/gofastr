// Package peerish mirrors core/moduleproto's Peer serve path, reduced
// to the shape: a read loop, a dispatch that spawns a goroutine per
// inbound frame, and a handler registry consulted by method name.
// badServe is the pre-fix buildResponse/serveNotification shape; the
// fixed spelling routes the call through a recovered runner.
package peerish

import (
	"context"
	"errors"
	"io"
)

type Frame struct {
	Method string
	Params []byte
}

type Handler func(ctx context.Context, params []byte) error

type Reader interface{ ReadFrame() (*Frame, error) }

type Peer struct {
	r        Reader
	handlers map[string]Handler
}

// readLoop is the seed: a for-loop blocking on a read.
func (p *Peer) readLoop() error {
	for {
		f, err := p.r.ReadFrame()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		p.dispatch(f)
	}
}

func (p *Peer) dispatch(f *Frame) {
	if f.Method == "notify" {
		p.dispatchNotification(f)
		return
	}
	go func() {
		_ = p.buildResponse(f)
	}()
}

// buildResponse is the pre-fix shape: the map-registered handler is
// called directly, in a goroutine, with no recover anywhere.
func (p *Peer) buildResponse(f *Frame) error {
	h, ok := p.handlers[f.Method]
	if !ok {
		return errors.New("method not found")
	}
	return h(context.Background(), f.Params) // want `recovercallback: h is invoked with no recover in scope`
}

// buildResponseFixed is the b79942f7 spelling: the call moved into a
// recovered runner (runHandler).
func (p *Peer) buildResponseFixed(f *Frame) error {
	h, ok := p.handlers[f.Method]
	if !ok {
		return errors.New("method not found")
	}
	return runHandler(h, f.Params)
}

func runHandler(h Handler, params []byte) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = errors.New("internal error")
		}
	}()
	return h(context.Background(), params)
}

// serveNotificationInline is the inline (non-goroutine) arm: the
// readLoop reaches it synchronously through dispatch, so it is on the
// dispatch path with no net all the same.
func (p *Peer) serveNotificationInline(f *Frame) {
	h, ok := p.handlers[f.Method]
	if !ok {
		return
	}
	_ = h(context.Background(), f.Params) // want `recovercallback: h is invoked with no recover in scope`
}

func (p *Peer) dispatchNotification(f *Frame) {
	if f.Method == "cancel" {
		p.serveNotificationInline(f)
	}
}
