package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"runtime/debug"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

const sseSubBufferSize = 16

const maxResourceURIBytes = 2048

const maxResourceSubscriptions = 1024

const maxResourceSubscriptionsPerCaller = maxResourceSubscriptions / 4

const defaultSSEStreamLimit = 1024

const maxResourceSubscriptionPairs = 4 * maxResourceSubscriptions

type sseNotification struct {
	method   string
	params   any
	itemGate func(ctx context.Context) error
}

type sseSubscriber struct {
	ch      chan sseNotification
	ctx     context.Context
	seatKey string
	// filter, when set, keeps a notification the listen stream would not deliver out of its buffer.
	filter *listenFilter
}

func (s *Server) addSSESubscriber(ctx context.Context, seatKey string) (sub *sseSubscriber, ok bool) {
	return s.addFilteredSSESubscriber(ctx, seatKey, nil)
}

func (s *Server) addFilteredSSESubscriber(ctx context.Context, seatKey string, filter *listenFilter) (sub *sseSubscriber, ok bool) {
	s.sseMu.Lock()
	defer s.sseMu.Unlock()
	if s.sseSubs == nil {
		s.sseSubs = make(map[*sseSubscriber]struct{})
	}
	if s.sseSeatOrder == nil {
		s.sseSeatOrder = make(map[string][]*sseSubscriber)
	}
	if limit := s.streamLimitLocked(); limit >= 0 && len(s.sseSubs) >= limit {
		max := s.seatCapLocked()
		canReplace := max > 0 && len(s.sseSeatOrder[seatKey]) >= max && s.sseSeatOverflow == SeatOverflowEvictOldest
		if len(s.sseSubs) > limit || !canReplace {
			return nil, false
		}
	}
	if max := s.seatCapLocked(); max > 0 && len(s.sseSeatOrder[seatKey]) >= max {
		if s.sseSeatOverflow != SeatOverflowEvictOldest || len(s.sseSeatOrder[seatKey]) == 0 {
			return nil, false
		}
		oldest := s.sseSeatOrder[seatKey][0]
		s.spliceSeatLocked(oldest)
		delete(s.sseSubs, oldest)
		close(oldest.ch)
		s.expireResourceSubsIfIdleLocked()
	}
	sub = &sseSubscriber{
		ch:      make(chan sseNotification, sseSubBufferSize),
		ctx:     ctx,
		seatKey: seatKey,
		filter:  filter,
	}
	s.sseSubs[sub] = struct{}{}
	s.sseSeatOrder[seatKey] = append(s.sseSeatOrder[seatKey], sub)
	return sub, true
}

func (s *Server) removeSSESubscriber(sub *sseSubscriber) {
	s.sseMu.Lock()
	delete(s.sseSubs, sub)
	s.spliceSeatLocked(sub)
	s.expireResourceSubsIfIdleLocked()
	s.sseMu.Unlock()
}

func (s *Server) expireResourceSubsIfIdleLocked() {
	if len(s.sseSubs) == 0 {
		clear(s.resourceSubs)
		clear(s.resourceSubsBy)
		clear(s.listenSubs)
		clear(s.listenSubsBy)
	}
}

type resourceSubKey struct{ caller, uri string }

func subscriptionCaller(ctx context.Context) string {
	r, _ := RequestFromContext(ctx)
	caller := sseSeatKey(ctx, r)
	const peer = "peer:"
	if !strings.HasPrefix(caller, peer) {
		return caller
	}
	host := strings.TrimPrefix(caller, peer)
	ip := net.ParseIP(host)
	if ip == nil || ip.To4() != nil || ip.To16() == nil {
		return caller
	}
	mask := net.CIDRMask(64, 128)
	return peer + ip.Mask(mask).String()
}

func (s *Server) notifySubscribers(n sseNotification) {
	s.sseMu.Lock()
	defer s.sseMu.Unlock()
	for sub := range s.sseSubs {
		if sub.filter != nil && !sub.filter.accepts(n) {
			continue
		}
		select {
		case sub.ch <- n:
		default:
			s.spliceSeatLocked(sub)
			delete(s.sseSubs, sub)
			close(sub.ch)
			s.expireResourceSubsIfIdleLocked()
		}
	}
}

func (s *Server) subscriberMayReceive(ctx context.Context, n sseNotification) bool {
	if !gateAllows(s.checkServerGate, ctx) {
		return false
	}
	if n.itemGate != nil && !gateAllows(n.itemGate, ctx) {
		return false
	}
	return true
}

func gateAllows(gate func(ctx context.Context) error, ctx context.Context) (ok bool) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("panic in MCP notification gate recovered", "operation", "notification gate",
				"panic", textsafe.Recovered(rec), "stack", string(debug.Stack()))
			ok = false
		}
	}()
	return gate(ctx) == nil
}

type jsonrpcNotification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type resourceUpdatedParams struct {
	URI string `json:"uri"`
}

// NotifyToolsListChanged sends notifications/tools/list_changed, with no item gate, to every stream the server-wide gate allows.
func (s *Server) NotifyToolsListChanged() {
	s.notifySubscribers(sseNotification{method: "notifications/tools/list_changed"})
}

// NotifyResourcesListChanged sends notifications/resources/list_changed, with no item gate, to every stream the server-wide gate allows.
func (s *Server) NotifyResourcesListChanged() {
	s.notifySubscribers(sseNotification{method: "notifications/resources/list_changed"})
}

// NotifyPromptsListChanged sends notifications/prompts/list_changed, with no item gate, to every stream the server-wide gate allows.
func (s *Server) NotifyPromptsListChanged() {
	s.notifySubscribers(sseNotification{method: "notifications/prompts/list_changed"})
}

// NotifyResourceUpdated sends notifications/resources/updated for a subscribed uri to the streams its resource gate and the server-wide gate allow; it is a no-op with no active subscription.
func (s *Server) NotifyResourceUpdated(uri string) {
	s.sseMu.Lock()
	n := s.resourceSubs[uri] + s.listenSubs[uri]
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

func (s *Server) handleResourcesSubscribe(ctx context.Context, req Request) Response {
	if req.Params == nil {
		return newErrorResponse(req.ID, ErrInvalidParams, "missing params")
	}
	var params resourcesReadParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return newErrorResponse(req.ID, ErrInvalidParams, "invalid params: "+handler.JSONErrorDetail(err).Error())
	}
	if params.URI == "" {
		return newErrorResponse(req.ID, ErrInvalidParams, "missing resource uri")
	}
	if len(params.URI) > maxResourceURIBytes {
		return newErrorResponse(req.ID, ErrInvalidParams,
			fmt.Sprintf("resource uri exceeds %d bytes", maxResourceURIBytes))
	}
	s.sseMu.Lock()
	if s.resourceSubs == nil {
		s.resourceSubs = make(map[string]int)
	}
	if !s.uriHeldLocked(params.URI) && s.distinctURIsLocked() >= maxResourceSubscriptions {
		s.sseMu.Unlock()
		return newErrorResponse(req.ID, ErrInvalidParams,
			fmt.Sprintf("too many distinct subscribed resource uris (max %d)", maxResourceSubscriptions))
	}
	if s.resourceSubsBy == nil {
		s.resourceSubsBy = make(map[resourceSubKey]int)
	}
	key := resourceSubKey{caller: subscriptionCaller(ctx), uri: params.URI}
	if !s.pairHeldLocked(key) {
		if len(s.resourceSubsBy) >= maxResourceSubscriptionPairs || s.pairsLocked() >= maxResourceSubscriptionPairs {
			s.sseMu.Unlock()
			return newErrorResponse(req.ID, ErrInvalidParams,
				fmt.Sprintf("too many resource subscriptions (max %d)", maxResourceSubscriptionPairs))
		}
		if s.callerPairsLocked(key.caller) >= maxResourceSubscriptionsPerCaller {
			s.sseMu.Unlock()
			return newErrorResponse(req.ID, ErrInvalidParams,
				fmt.Sprintf("too many distinct subscribed resource uris for this caller (max %d)", maxResourceSubscriptionsPerCaller))
		}
	}
	s.resourceSubs[params.URI]++
	s.resourceSubsBy[key]++
	s.sseMu.Unlock()
	return newSuccessResponse(req.ID, map[string]any{})
}

func (s *Server) handleResourcesUnsubscribe(ctx context.Context, req Request) Response {
	if req.Params == nil {
		return newErrorResponse(req.ID, ErrInvalidParams, "missing params")
	}
	var params resourcesReadParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return newErrorResponse(req.ID, ErrInvalidParams, "invalid params: "+handler.JSONErrorDetail(err).Error())
	}
	if params.URI == "" {
		return newErrorResponse(req.ID, ErrInvalidParams, "missing resource uri")
	}
	key := resourceSubKey{caller: subscriptionCaller(ctx), uri: params.URI}
	s.sseMu.Lock()
	if own := s.resourceSubsBy[key]; own > 0 {
		if own == 1 {
			delete(s.resourceSubsBy, key)
		} else {
			s.resourceSubsBy[key] = own - 1
		}
		if n := s.resourceSubs[params.URI]; n <= 1 {
			delete(s.resourceSubs, params.URI)
		} else {
			s.resourceSubs[params.URI] = n - 1
		}
	}
	s.sseMu.Unlock()
	return newSuccessResponse(req.ID, map[string]any{})
}
