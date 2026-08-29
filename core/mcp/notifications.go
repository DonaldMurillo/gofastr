package mcp

import (
	"context"
	"encoding/json"
)

// sseSubBufferSize is the bounded per-subscriber notification buffer. A
// subscriber whose stream loop stops draining it for this many
// notifications is dropped (see notifySubscribers): the publisher is
// never blocked and the buffer is never grown.
const sseSubBufferSize = 16

// sseNotification is one server-initiated MCP notification queued for
// delivery to the SSE subscriber streams.
type sseNotification struct {
	// method is the JSON-RPC notification method, e.g.
	// "notifications/tools/list_changed".
	method string
	// params is the optional params object; nil for the list_changed
	// notifications, which carry no payload.
	params any
	// itemGate, when non-nil, is the per-item gate a subscriber's
	// caller must pass to receive this notification — for
	// notifications/resources/updated, the resource's own
	// WithResourceGate. It is evaluated per subscriber, at delivery
	// time, against the identity the subscriber's GET request
	// presented. See subscriberMayReceive for why delivery time and
	// not subscribe time.
	itemGate func(ctx context.Context) error
}

// sseSubscriber is one held-open GET stream. The channel is written only
// by notifySubscribers (under s.sseMu, never blocking) and read only by
// the stream's own write loop in sseGetHandler.
type sseSubscriber struct {
	ch chan sseNotification
	// ctx carries the caller identity from the subscriber's GET
	// request: the request context, enriched the same way the POST
	// path enriches it, with the inbound *http.Request stashed via
	// WithRequest. Gates are evaluated against it at delivery time.
	ctx context.Context
}

// addSSESubscriber registers a stream for notification delivery and
// returns it. Called from sseGetHandler after the origin/Host gate.
func (s *Server) addSSESubscriber(ctx context.Context) *sseSubscriber {
	sub := &sseSubscriber{
		ch:  make(chan sseNotification, sseSubBufferSize),
		ctx: ctx,
	}
	s.sseMu.Lock()
	if s.sseSubs == nil {
		s.sseSubs = make(map[*sseSubscriber]struct{})
	}
	s.sseSubs[sub] = struct{}{}
	s.sseMu.Unlock()
	return sub
}

// removeSSESubscriber unregisters a stream. Called from sseGetHandler's
// exit path (client disconnect or dropped-for-backpressure). It does not
// close sub.ch: only notifySubscribers closes channels, and only under
// sseMu, so a fan-out holding the registry can never race a send onto a
// closed channel. An unregistered, unclosed channel is simply garbage.
func (s *Server) removeSSESubscriber(sub *sseSubscriber) {
	s.sseMu.Lock()
	delete(s.sseSubs, sub)
	s.sseMu.Unlock()
}

// notifySubscribers fans a notification out to every live subscriber.
// It never blocks and never runs a gate: enqueueing is a non-blocking
// channel send, and the gates run in each subscriber's own write loop
// (subscriberMayReceive), so a slow or panicking gate can stall only
// that subscriber's stream, not the publisher and not the other
// subscribers.
func (s *Server) notifySubscribers(n sseNotification) {
	s.sseMu.Lock()
	defer s.sseMu.Unlock()
	for sub := range s.sseSubs {
		select {
		case sub.ch <- n:
		default:
			// Backpressure: this subscriber's buffer is full, which
			// means its write loop has stopped draining — a stalled or
			// slow client. Do NOT block (the caller is whatever server
			// code raised the notification, and blocking it would stall
			// every other subscriber too) and do NOT grow the buffer:
			// drop the subscriber and close its stream. list_changed is
			// idempotent and resources/updated sends the client back to
			// resources/read for current state, so a client that
			// reconnects is correct again after re-listing.
			delete(s.sseSubs, sub)
			close(sub.ch)
		}
	}
}

// subscriberMayReceive decides whether one stream may see one
// notification. Both gates are evaluated HERE, in the subscriber's
// write loop at delivery time, against the identity stored at subscribe
// time — never once at subscribe time — because a gate can depend on
// state that changes during a long-lived connection (a session revoked
// mid-stream must stop receiving).
//
// The two gates, in order:
//
//   - the server-wide gate (SetGate): a caller refused wholesale learns
//     nothing, not even that something changed. list_changed carries no
//     payload, so it is safe to broadcast — but only past this gate.
//   - the notification's item gate (a resource's WithResourceGate):
//     notifications/resources/updated carries a URI, so it must reach
//     only subscribers whose gate would let them read that resource.
//     Pushing it past a refusing gate would disclose the existence and
//     URI of a gated resource — the same disclosure the list methods
//     prevent by hiding gated items and paging the post-gate set.
func (s *Server) subscriberMayReceive(sub *sseSubscriber, n sseNotification) bool {
	if !gateAllows(s.checkServerGate, sub.ctx) {
		return false
	}
	if n.itemGate != nil && !gateAllows(n.itemGate, sub.ctx) {
		return false
	}
	return true
}

// gateAllows runs a gate under a recover guard, failing CLOSED: a
// panicking gate refuses the notification instead of unwinding the
// subscriber's stream loop. Same contract as checkPromptGate — a panic
// in app-supplied gate code must never kill a transport loop.
func gateAllows(gate func(ctx context.Context) error, ctx context.Context) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	return gate(ctx) == nil
}

// jsonrpcNotification is a server-initiated JSON-RPC message: a method
// with optional params and no id. Notifications never get a response.
type jsonrpcNotification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// resourceUpdatedParams is the params object for
// notifications/resources/updated: the uri that changed.
type resourceUpdatedParams struct {
	URI string `json:"uri"`
}

// NotifyToolsListChanged raises notifications/tools/list_changed on
// every eligible subscriber stream: any connected client whose
// server-wide gate allows it should re-issue tools/list.
//
// RegisterTool fires this automatically, so app code needs it only
// after mutating tool visibility some other way.
func (s *Server) NotifyToolsListChanged() {
	s.notifySubscribers(sseNotification{method: "notifications/tools/list_changed"})
}

// NotifyResourcesListChanged raises notifications/resources/list_changed
// on every eligible subscriber stream. RegisterResource and
// RegisterResourceTemplate fire it automatically (the spec folds
// templates under the one `resources` capability, and has no separate
// template list_changed).
func (s *Server) NotifyResourcesListChanged() {
	s.notifySubscribers(sseNotification{method: "notifications/resources/list_changed"})
}

// NotifyPromptsListChanged raises notifications/prompts/list_changed on
// every eligible subscriber stream. RegisterPrompt fires it
// automatically.
func (s *Server) NotifyPromptsListChanged() {
	s.notifySubscribers(sseNotification{method: "notifications/prompts/list_changed"})
}

// NotifyResourceUpdated raises notifications/resources/updated for uri
// on the streams whose caller passes the resource's own gate
// (WithResourceGate) and the server-wide gate, both evaluated at
// delivery time. A uri with no registered resource carries no item gate
// and reaches every server-gate-passing subscriber — the spec allows
// update notices for resources a client subscribed to before they were
// registered.
//
// It is a no-op unless at least one resources/subscribe is active for
// uri: the spec has servers sending updates only for subscribed
// resources. Subscribe first (an MCP client does this itself via
// resources/subscribe).
func (s *Server) NotifyResourceUpdated(uri string) {
	s.sseMu.Lock()
	n := s.resourceSubs[uri]
	s.sseMu.Unlock()
	if n == 0 {
		return
	}
	var gate func(ctx context.Context) error
	s.mu.RLock()
	if res, ok := s.resources[uri]; ok {
		gate = res.gate
	}
	s.mu.RUnlock()
	s.notifySubscribers(sseNotification{
		method:   "notifications/resources/updated",
		params:   resourceUpdatedParams{URI: uri},
		itemGate: gate,
	})
}

// handleResourcesSubscribe records a resources/subscribe request: from
// here on, NotifyResourceUpdated(uri) delivers to the eligible streams.
//
// Transport limit, on purpose: this HTTP transport has no session id
// linking a POST to a particular GET stream, so the subscription is
// connection-agnostic — a per-uri count, not a per-stream set. A uri
// any client subscribed to gets updates on every eligible stream. The
// per-subscriber gates remain the security boundary; this is the same
// class of limit the multi-replica deployment already has (see the
// notifications section of framework/docs/content/mcp.md). The server
// may subscribe to a uri before it is registered, per spec.
func (s *Server) handleResourcesSubscribe(_ context.Context, req Request) Response {
	if req.Params == nil {
		return newErrorResponse(req.ID, ErrInvalidParams, "missing params")
	}
	var params resourcesReadParams // same {"uri"} shape
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return newErrorResponse(req.ID, ErrInvalidParams, "invalid params: "+err.Error())
	}
	if params.URI == "" {
		return newErrorResponse(req.ID, ErrInvalidParams, "missing resource uri")
	}
	s.sseMu.Lock()
	if s.resourceSubs == nil {
		s.resourceSubs = make(map[string]int)
	}
	s.resourceSubs[params.URI]++
	s.sseMu.Unlock()
	return newSuccessResponse(req.ID, map[string]any{})
}

// handleResourcesUnsubscribe releases one resources/subscribe. The count
// per uri is decremented; at zero the uri stops receiving updates.
// Unsubscribing a uri nobody subscribed to is a no-op, not an error, per
// spec.
func (s *Server) handleResourcesUnsubscribe(_ context.Context, req Request) Response {
	if req.Params == nil {
		return newErrorResponse(req.ID, ErrInvalidParams, "missing params")
	}
	var params resourcesReadParams // same {"uri"} shape
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return newErrorResponse(req.ID, ErrInvalidParams, "invalid params: "+err.Error())
	}
	if params.URI == "" {
		return newErrorResponse(req.ID, ErrInvalidParams, "missing resource uri")
	}
	s.sseMu.Lock()
	if n := s.resourceSubs[params.URI]; n <= 1 {
		delete(s.resourceSubs, params.URI)
	} else {
		s.resourceSubs[params.URI] = n - 1
	}
	s.sseMu.Unlock()
	return newSuccessResponse(req.ID, map[string]any{})
}
