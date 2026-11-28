package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// ErrTooManyStreams answers a subscriptions/listen refused because its caller already holds the most streams its seat allows; the HTTP status is 429.
const ErrTooManyStreams = -32000

// MetaSubscriptionID is the `_meta` key naming the subscriptions/listen request a notification belongs to.
const MetaSubscriptionID = "io.modelcontextprotocol/subscriptionId"

type listenFilter struct {
	id        any
	tools     bool
	prompts   bool
	resources bool
	uris      map[string]bool
}

type listenNotifications struct {
	ToolsListChanged      bool     `json:"toolsListChanged,omitempty"`
	PromptsListChanged    bool     `json:"promptsListChanged,omitempty"`
	ResourcesListChanged  bool     `json:"resourcesListChanged,omitempty"`
	ResourceSubscriptions []string `json:"resourceSubscriptions,omitempty"`
}

type listenKey struct{}

func listenAvailable(ctx context.Context) bool {
	v, _ := ctx.Value(listenKey{}).(bool)
	return v
}

func decodeListenParams(raw json.RawMessage) (listenNotifications, error) {
	var p struct {
		Notifications json.RawMessage `json:"notifications"`
	}
	var n listenNotifications
	if err := json.Unmarshal(raw, &p); err != nil {
		return n, handler.JSONErrorDetail(err)
	}
	if len(p.Notifications) == 0 || bytes.Equal(bytes.TrimSpace(p.Notifications), []byte("null")) {
		return n, nil
	}
	if err := json.Unmarshal(p.Notifications, &n); err != nil {
		return n, fmt.Errorf("notifications: %w", handler.JSONErrorDetail(err))
	}
	if len(n.ResourceSubscriptions) > maxResourceSubscriptionsPerCaller {
		return n, fmt.Errorf("notifications.resourceSubscriptions holds more than %d uris", maxResourceSubscriptionsPerCaller)
	}
	seen := make(map[string]bool, len(n.ResourceSubscriptions))
	uris := n.ResourceSubscriptions[:0:0]
	for _, uri := range n.ResourceSubscriptions {
		if uri == "" || len(uri) > maxResourceURIBytes {
			return n, fmt.Errorf("notifications.resourceSubscriptions holds an empty uri or one over %d bytes", maxResourceURIBytes)
		}
		if !seen[uri] {
			seen[uri] = true
			uris = append(uris, uri)
		}
	}
	n.ResourceSubscriptions = uris
	return n, nil
}

func (s *Server) serveListen(w http.ResponseWriter, r *http.Request, ctx context.Context, req Request) {
	answer := func(resp Response) {
		resp.modern = true
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(httpStatusFor(resp))
		_, _ = w.Write(encodeResponse(resp))
	}
	if resp, refused := refuseEnvelope(req); refused {
		answer(resp)
		return
	}
	if err := s.checkServerGate(ctx); err != nil {
		answer(Response{JSONRPC: "2.0", ID: req.ID, Error: gateRefusal(err)})
		return
	}
	want, err := decodeListenParams(req.Params)
	if err != nil {
		answer(newErrorResponse(req.ID, ErrInvalidParams, "invalid params: "+err.Error()))
		return
	}
	f := &listenFilter{
		id: req.ID, tools: want.ToolsListChanged, prompts: want.PromptsListChanged,
		resources: want.ResourcesListChanged, uris: make(map[string]bool, len(want.ResourceSubscriptions)),
	}
	for _, uri := range want.ResourceSubscriptions {
		f.uris[uri] = true
	}
	sub, ok := s.addFilteredSSESubscriber(ctx, sseSeatKey(ctx, r), f)
	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write(encodeResponse(newErrorResponse(req.ID, ErrTooManyStreams, i18n.ReasonTooManyStreams.Text(ctx))))
		return
	}
	defer s.removeSSESubscriber(sub)
	caller := subscriptionCaller(ctx)
	if msg := s.holdListenURIs(caller, want.ResourceSubscriptions); msg != "" {
		answer(newErrorResponse(req.ID, ErrInvalidParams, msg))
		return
	}
	defer s.releaseListenURIs(caller, want.ResourceSubscriptions)
	ack, err := json.Marshal(jsonrpcNotification{JSONRPC: "2.0", Method: "notifications/subscriptions/acknowledged", Params: map[string]any{
		"_meta":         map[string]any{MetaSubscriptionID: req.ID},
		"notifications": want,
	}})
	if err != nil {
		answer(newErrorResponse(req.ID, ErrInternalError, "internal error"))
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if final := s.pumpSSE(w, r, sub, "message", string(ack), f.encode); final != nil {
		closing := newSuccessResponse(req.ID, map[string]any{
			"resultType": "complete",
			"_meta":      map[string]any{MetaSubscriptionID: req.ID},
		})
		final("message", string(bytes.TrimSuffix(encodeResponse(closing), []byte("\n"))))
	}
}

// accepts reports whether the filter delivers n, so notifySubscribers keeps everything else out of the stream's buffer.
func (f *listenFilter) accepts(n sseNotification) bool {
	switch n.method {
	case "notifications/tools/list_changed":
		return f.tools
	case "notifications/prompts/list_changed":
		return f.prompts
	case "notifications/resources/list_changed":
		return f.resources
	case "notifications/resources/updated":
		p, ok := n.params.(resourceUpdatedParams)
		return ok && f.uris[p.URI]
	}
	return false
}

func (f *listenFilter) encode(n sseNotification) ([]byte, bool) {
	params := map[string]any{}
	switch n.method {
	case "notifications/tools/list_changed":
		if !f.tools {
			return nil, false
		}
	case "notifications/prompts/list_changed":
		if !f.prompts {
			return nil, false
		}
	case "notifications/resources/list_changed":
		if !f.resources {
			return nil, false
		}
	case "notifications/resources/updated":
		p, ok := n.params.(resourceUpdatedParams)
		if !ok || !f.uris[p.URI] {
			return nil, false
		}
		params["uri"] = p.URI
	default:
		return nil, false
	}
	params["_meta"] = map[string]any{MetaSubscriptionID: f.id}
	data, err := json.Marshal(jsonrpcNotification{JSONRPC: "2.0", Method: n.method, Params: params})
	return data, err == nil
}

// holdListenURIs counts uris as listened to by caller, in counters of their own so resources/unsubscribe cannot
// release them, under the limits resources/subscribe applies to both kinds together. It answers why when they would be
// passed, and holds nothing then.
func (s *Server) holdListenURIs(caller string, uris []string) string {
	if len(uris) == 0 {
		return ""
	}
	s.sseMu.Lock()
	defer s.sseMu.Unlock()
	if s.listenSubs == nil {
		s.listenSubs = make(map[string]int)
	}
	if s.listenSubsBy == nil {
		s.listenSubsBy = make(map[resourceSubKey]int)
	}
	freshURIs, freshPairs := 0, 0
	for _, uri := range uris {
		if !s.uriHeldLocked(uri) {
			freshURIs++
		}
		if !s.pairHeldLocked(resourceSubKey{caller: caller, uri: uri}) {
			freshPairs++
		}
	}
	if s.distinctURIsLocked()+freshURIs > maxResourceSubscriptions {
		return fmt.Sprintf("too many distinct subscribed resource uris (max %d)", maxResourceSubscriptions)
	}
	if s.pairsLocked()+freshPairs > maxResourceSubscriptionPairs {
		return fmt.Sprintf("too many resource subscriptions (max %d)", maxResourceSubscriptionPairs)
	}
	if s.callerPairsLocked(caller)+freshPairs > maxResourceSubscriptionsPerCaller {
		return fmt.Sprintf("too many distinct subscribed resource uris for this caller (max %d)", maxResourceSubscriptionsPerCaller)
	}
	for _, uri := range uris {
		s.listenSubs[uri]++
		s.listenSubsBy[resourceSubKey{caller: caller, uri: uri}]++
	}
	return ""
}

func (s *Server) releaseListenURIs(caller string, uris []string) {
	if len(uris) == 0 {
		return
	}
	s.sseMu.Lock()
	defer s.sseMu.Unlock()
	for _, uri := range uris {
		if n := s.listenSubs[uri]; n <= 1 {
			delete(s.listenSubs, uri)
		} else {
			s.listenSubs[uri] = n - 1
		}
		key := resourceSubKey{caller: caller, uri: uri}
		if n := s.listenSubsBy[key]; n <= 1 {
			delete(s.listenSubsBy, key)
		} else {
			s.listenSubsBy[key] = n - 1
		}
	}
}

func (s *Server) uriHeldLocked(uri string) bool {
	return s.resourceSubs[uri] > 0 || s.listenSubs[uri] > 0
}

func (s *Server) pairHeldLocked(key resourceSubKey) bool {
	return s.resourceSubsBy[key] > 0 || s.listenSubsBy[key] > 0
}

// distinctURIsLocked counts the URIs either kind of subscription holds, each once.
func (s *Server) distinctURIsLocked() int {
	n := len(s.resourceSubs)
	for uri := range s.listenSubs {
		if s.resourceSubs[uri] == 0 {
			n++
		}
	}
	return n
}

// pairsLocked counts the caller-URI pairs either kind holds, each once.
func (s *Server) pairsLocked() int {
	n := len(s.resourceSubsBy)
	for key := range s.listenSubsBy {
		if s.resourceSubsBy[key] == 0 {
			n++
		}
	}
	return n
}

// callerPairsLocked counts the distinct URIs caller holds across both kinds.
func (s *Server) callerPairsLocked(caller string) int {
	n := 0
	for key := range s.resourceSubsBy {
		if key.caller == caller {
			n++
		}
	}
	for key := range s.listenSubsBy {
		if key.caller == caller && s.resourceSubsBy[key] == 0 {
			n++
		}
	}
	return n
}
