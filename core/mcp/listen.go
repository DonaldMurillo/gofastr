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
	sub, ok := s.addSSESubscriber(ctx, sseSeatKey(ctx, r))
	if !ok {
		i18n.HTTPError(w, r, http.StatusTooManyRequests, i18n.ReasonTooManyStreams)
		return
	}
	defer s.removeSSESubscriber(sub)
	caller := subscriptionCaller(ctx)
	if msg := s.holdListenURIs(caller, want.ResourceSubscriptions); msg != "" {
		answer(newErrorResponse(req.ID, ErrInvalidParams, msg))
		return
	}
	defer s.releaseListenURIs(caller, want.ResourceSubscriptions)

	f := &listenFilter{
		id: req.ID, tools: want.ToolsListChanged, prompts: want.PromptsListChanged,
		resources: want.ResourcesListChanged, uris: make(map[string]bool, len(want.ResourceSubscriptions)),
	}
	for _, uri := range want.ResourceSubscriptions {
		f.uris[uri] = true
	}
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
	s.pumpSSE(w, r, sub, "message", string(ack), f.encode)
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

// holdListenURIs counts uris as subscribed by caller under the limits resources/subscribe applies, answering why when
// they would be passed and holding nothing then.
func (s *Server) holdListenURIs(caller string, uris []string) string {
	if len(uris) == 0 {
		return ""
	}
	s.sseMu.Lock()
	defer s.sseMu.Unlock()
	if s.resourceSubs == nil {
		s.resourceSubs = make(map[string]int)
	}
	if s.resourceSubsBy == nil {
		s.resourceSubsBy = make(map[resourceSubKey]int)
	}
	freshURIs, freshPairs := 0, 0
	for _, uri := range uris {
		if _, ok := s.resourceSubs[uri]; !ok {
			freshURIs++
		}
		if _, ok := s.resourceSubsBy[resourceSubKey{caller: caller, uri: uri}]; !ok {
			freshPairs++
		}
	}
	if len(s.resourceSubs)+freshURIs > maxResourceSubscriptions {
		return fmt.Sprintf("too many distinct subscribed resource uris (max %d)", maxResourceSubscriptions)
	}
	if len(s.resourceSubsBy)+freshPairs > maxResourceSubscriptionPairs {
		return fmt.Sprintf("too many resource subscriptions (max %d)", maxResourceSubscriptionPairs)
	}
	own := 0
	for k := range s.resourceSubsBy {
		if k.caller == caller {
			own++
		}
	}
	if own+freshPairs > maxResourceSubscriptionsPerCaller {
		return fmt.Sprintf("too many distinct subscribed resource uris for this caller (max %d)", maxResourceSubscriptionsPerCaller)
	}
	for _, uri := range uris {
		s.resourceSubs[uri]++
		s.resourceSubsBy[resourceSubKey{caller: caller, uri: uri}]++
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
		if n := s.resourceSubs[uri]; n <= 1 {
			delete(s.resourceSubs, uri)
		} else {
			s.resourceSubs[uri] = n - 1
		}
		key := resourceSubKey{caller: caller, uri: uri}
		if n := s.resourceSubsBy[key]; n <= 1 {
			delete(s.resourceSubsBy, key)
		} else {
			s.resourceSubsBy[key] = n - 1
		}
	}
}
