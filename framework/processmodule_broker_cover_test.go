package framework

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/moduleproto"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/event"
)

// This file adds unit coverage for the PURE request-shaping / result-mapping
// helpers in processmodule_broker.go. None of these tests spawn a child,
// touch Postgres, or perform any OS-specific syscall — they exercise the
// exported/unexported helpers directly.

// ---- sortParam / selectParam ----

func TestSortParam_decodesStringAndList(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`"name"`, "name"},
		{`["a","b"]`, "a,b"},
		{`[]`, ""},
		{``, ""},
		{`not-json`, ""},
		{`123`, ""}, // neither string nor []string
	}
	for _, c := range cases {
		if got := sortParam(json.RawMessage(c.in)); got != c.want {
			t.Errorf("sortParam(%s) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSelectParam_decodesStringAndList(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`"id,title"`, "id,title"},
		{`["id","title"]`, "id,title"},
		{`[]`, ""},
		{``, ""},
		{`{}`, ""},
	}
	for _, c := range cases {
		if got := selectParam(json.RawMessage(c.in)); got != c.want {
			t.Errorf("selectParam(%s) = %q, want %q", c.in, got, c.want)
		}
	}
}

// ---- singleID ----

func TestSingleID_rejectsEmptyReturnsDenial(t *testing.T) {
	for _, ids := range [][]string{nil, {""}} {
		_, err := singleID(ids)
		if err == nil {
			t.Errorf("singleID(%v) must error", ids)
			continue
		}
		var we *moduleproto.Error
		if !errors.As(err, &we) || we.Code != moduleproto.CodeCapabilityDenied {
			t.Errorf("singleID(%v) error = %+v, want CodeCapabilityDenied", ids, we)
		}
	}
}

func TestSingleID_returnsFirst(t *testing.T) {
	id, err := singleID([]string{"a", "b"})
	if err != nil || id != "a" {
		t.Fatalf(`singleID(["a","b"]) = %q,%v, want "a",nil`, id, err)
	}
}

// ---- rawBody ----

func TestRawBody_nilAndValue(t *testing.T) {
	if got := rawBody(nil); got != nil {
		t.Errorf("rawBody(nil) = %v, want nil", got)
	}
	in := json.RawMessage(`{"a":1}`)
	got := rawBody(in)
	if string(got.(json.RawMessage)) != `{"a":1}` {
		t.Errorf("rawBody value = %v, want passthrough", got)
	}
}

// ---- mapMutationResult ----

func TestMapMutationResult_envelope(t *testing.T) {
	res := mapMutationResult(map[string]any{"data": map[string]any{"id": 7}})
	if res.Affected != 1 {
		t.Errorf("Affected = %d, want 1", res.Affected)
	}
	if string(res.Rows) == "" || !strings.Contains(string(res.Rows), `"id":7`) {
		t.Errorf("Rows = %s, want data payload", string(res.Rows))
	}
}

func TestMapMutationResult_nil(t *testing.T) {
	res := mapMutationResult(nil)
	if res.Affected != 1 || res.Rows != nil {
		t.Errorf("nil mapMutationResult = %+v, want Affected=1 nil Rows", res)
	}
}

func TestMapMutationResult_rawValue(t *testing.T) {
	res := mapMutationResult("plain")
	if res.Affected != 1 || string(res.Rows) != `"plain"` {
		t.Errorf("raw mapMutationResult = %+v, want Affected=1 quoted value", res)
	}
}

// ---- mapSearchResult ----

func TestMapSearchResult_envelopeData(t *testing.T) {
	r := mapSearchResult(map[string]any{
		"data":  []any{"x"},
		"total": float64(3),
	}).(moduleproto.SearchQueryResult)
	if r.Total != 3 || string(r.Results) == "" {
		t.Errorf("data-envelope search result = %+v", r)
	}
}
func TestMapSearchResult_envelopeWithoutData(t *testing.T) {
	// An envelope whose "data" key is absent: json.Marshal(nil) yields
	// "null", so Results carries that literal (the results-fallback only
	// fires when Marshal returns nil bytes, which it never does).
	r := mapSearchResult(map[string]any{
		"results": []any{"y"},
		"total":   float64(2),
	}).(moduleproto.SearchQueryResult)
	if r.Total != 2 {
		t.Errorf("Total = %d, want 2", r.Total)
	}
	if string(r.Results) != "null" {
		t.Errorf("Results = %s, want null (data absent)", string(r.Results))
	}
}

func TestMapSearchResult_rawValue(t *testing.T) {
	r := mapSearchResult(42).(moduleproto.SearchQueryResult)
	if string(r.Results) != "42" {
		t.Errorf("raw search result = %s, want 42", string(r.Results))
	}
}

// ---- mapQueryResult ----

func TestMapQueryResult_envelopeWithTotal(t *testing.T) {
	res, err := mapQueryResult(map[string]any{
		"data":  []any{map[string]any{"id": 1}},
		"total": float64(5),
	})
	if err != nil {
		t.Fatalf("mapQueryResult: %v", err)
	}
	qr := res.(moduleproto.EntityQueryResult)
	if qr.Total != 5 || string(qr.Rows) == "" {
		t.Errorf("query result = %+v", qr)
	}
}

func TestMapQueryResult_nonEnvelope(t *testing.T) {
	res, err := mapQueryResult("just-a-string")
	if err != nil {
		t.Fatalf("mapQueryResult: %v", err)
	}
	qr := res.(moduleproto.EntityQueryResult)
	if qr.Total != 0 || string(qr.Rows) != `"just-a-string"` {
		t.Errorf("non-envelope query result = %+v", qr)
	}
}

// ---- paramErr / brokerDeny / mapRedispatchErr ----

func TestParamErr_codeInvalidParams(t *testing.T) {
	err := paramErr("bad %s", "input")
	we, ok := err.(*moduleproto.Error)
	if !ok || we.Code != moduleproto.CodeInvalidParams {
		t.Fatalf("paramErr type/code = %T %+v", err, we)
	}
	if !strings.Contains(we.Message, "bad input") || !strings.Contains(we.Message, "broker") {
		t.Errorf("paramErr message = %q", we.Message)
	}
}

func TestBrokerDeny_codeCapabilityDenied(t *testing.T) {
	err := brokerDeny("denied %s", "x")
	we, ok := err.(*moduleproto.Error)
	if !ok || we.Code != moduleproto.CodeCapabilityDenied {
		t.Fatalf("brokerDeny type/code = %T %+v", err, we)
	}
	if !strings.Contains(we.Message, "denied x") {
		t.Errorf("brokerDeny message = %q", we.Message)
	}
}

func TestMapRedispatchErr_classifies401asDenial(t *testing.T) {
	err := mapRedispatchErr(errors.New("crud: status 401 Unauthorized"))
	we, ok := err.(*moduleproto.Error)
	if !ok || we.Code != moduleproto.CodeCapabilityDenied {
		t.Fatalf("401 want CodeCapabilityDenied, got %T %+v", err, we)
	}
}

func TestMapRedispatchErr_classifies403asDenial(t *testing.T) {
	err := mapRedispatchErr(errors.New("crud: status 403 Forbidden"))
	we, ok := err.(*moduleproto.Error)
	if !ok || we.Code != moduleproto.CodeCapabilityDenied {
		t.Fatalf("403 want CodeCapabilityDenied, got %T %+v", err, we)
	}
}

func TestMapRedispatchErr_otherIsInternal(t *testing.T) {
	err := mapRedispatchErr(errors.New("connection refused"))
	we, ok := err.(*moduleproto.Error)
	if !ok || we.Code != moduleproto.CodeInternalError {
		t.Fatalf("non-401/403 want CodeInternalError, got %T %+v", err, we)
	}
}

// ---- expandRaw ----

func TestExpandRaw_setsObjectKeys(t *testing.T) {
	q := url.Values{}
	expandRaw(q, json.RawMessage(`{"status":"open","n":3}`))
	if q.Get("status") != "open" || q.Get("n") != "3" {
		t.Errorf("expandRaw result = %q", q.Encode())
	}
}

func TestExpandRaw_ignoresNonObject(t *testing.T) {
	q := url.Values{}
	expandRaw(q, nil)                        // no-op
	expandRaw(q, json.RawMessage(`"x"`))     // not an object → no-op
	expandRaw(q, json.RawMessage(`[1,2]`))   // array → no-op
	expandRaw(q, json.RawMessage(`garbage`)) // invalid → no-op
	if len(q) != 0 {
		t.Errorf("non-object expandRaw added params: %q", q.Encode())
	}
}

// ---- entityQuerySuffix ----

func TestEntityQuerySuffix_emptyReturnsEmpty(t *testing.T) {
	if got := entityQuerySuffix(moduleproto.EntityQueryParams{}); got != "" {
		t.Errorf("empty params suffix = %q, want empty", got)
	}
}

func TestEntityQuerySuffix_assemblesAllParams(t *testing.T) {
	suffix := entityQuerySuffix(moduleproto.EntityQueryParams{
		Filter: json.RawMessage(`{"status":"open"}`),
		Sort:   json.RawMessage(`"created_at"`),
		Select: json.RawMessage(`["id","title"]`),
		Limit:  10,
		Offset: 5,
	})
	if !strings.HasPrefix(suffix, "?") {
		t.Fatalf("suffix missing ?: %q", suffix)
	}
	parsed, err := url.ParseQuery(strings.TrimPrefix(suffix, "?"))
	if err != nil {
		t.Fatalf("parse suffix: %v", err)
	}
	if parsed.Get("status") != "open" {
		t.Errorf("filter not expanded: %q", parsed.Get("status"))
	}
	if parsed.Get("sort") != "created_at" {
		t.Errorf("sort = %q", parsed.Get("sort"))
	}
	if parsed.Get("fields") != "id,title" {
		t.Errorf("fields = %q", parsed.Get("fields"))
	}
	if parsed.Get("limit") != "10" {
		t.Errorf("limit = %q", parsed.Get("limit"))
	}
	if parsed.Get("offset") != "5" {
		t.Errorf("offset = %q", parsed.Get("offset"))
	}
}

// ---- unmarshalParams ----

func TestUnmarshalParams_emptyErrors(t *testing.T) {
	if err := unmarshalParams(nil, &struct{}{}); err == nil {
		t.Error("unmarshalParams(nil) must error")
	}
}

func TestUnmarshalParams_malformedErrors(t *testing.T) {
	var dst struct{ X int }
	if err := unmarshalParams(json.RawMessage(`not-json`), &dst); err == nil {
		t.Error("unmarshalParams(garbage) must error")
	}
}

func TestUnmarshalParams_validPasses(t *testing.T) {
	var dst struct{ X int }
	if err := unmarshalParams(json.RawMessage(`{"x":7}`), &dst); err != nil || dst.X != 7 {
		t.Fatalf("unmarshalParams valid = %v, dst=%+v", err, dst)
	}
}

// ---- entityOp.verb ----

func TestEntityOp_verbMapping(t *testing.T) {
	cases := []struct {
		op   entityOp
		verb string
	}{
		{opQuery, "read"},
		{opCreate, "write"},
		{opUpdate, "write"},
		{opDelete, "delete"},
		{entityOp(99), "read"}, // default branch
	}
	for _, c := range cases {
		if got := c.op.verb(); got != c.verb {
			t.Errorf("op(%d).verb() = %q, want %q", c.op, got, c.verb)
		}
	}
}

// ---- parseEntityCall ----

func TestParseEntityCall_queryShape(t *testing.T) {
	params, _ := json.Marshal(moduleproto.EntityQueryParams{
		Entity: "articles",
		Caller: moduleproto.Caller{Subject: "u1"},
	})
	name, caller, err := parseEntityCall(opQuery, params)
	if err != nil || name != "articles" || caller.Subject != "u1" {
		t.Fatalf("parseEntityCall query = %q %+v %v", name, caller, err)
	}
}

func TestParseEntityCall_mutationShape(t *testing.T) {
	params, _ := json.Marshal(moduleproto.EntityMutationParams{
		Entity: "articles",
		IDs:    []string{"9"},
	})
	name, _, err := parseEntityCall(opUpdate, params)
	if err != nil || name != "articles" {
		t.Fatalf("parseEntityCall mutation = %q %v", name, err)
	}
}

func TestParseEntityCall_emptyParamsErrors(t *testing.T) {
	if _, _, err := parseEntityCall(opQuery, nil); err == nil {
		t.Error("parseEntityCall(nil) must error")
	}
}

// ---- snapshotRequest / moduleRole ----

func TestSnapshotRequest_carriesCredentials(t *testing.T) {
	r := snapshotRequest(delegationEntry{cookie: "sid=x", authorization: "Bearer t"})
	if r.Header.Get("Cookie") != "sid=x" {
		t.Errorf("Cookie = %q", r.Header.Get("Cookie"))
	}
	if r.Header.Get("Authorization") != "Bearer t" {
		t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
	}
}

func TestSnapshotRequest_emptyHasNoHeaders(t *testing.T) {
	r := snapshotRequest(delegationEntry{})
	if len(r.Header) != 0 {
		t.Errorf("empty entry headers = %v", r.Header)
	}
}
func TestWithBrokerPolicy_wiresPolicy(t *testing.T) {
	// A fresh empty policy is a legal (fail-closed) wiring; just exercise the setter.
	NewBroker(nil, nil, nil, "", WithBrokerPolicy(access.NewRolePolicy()))
}

// ---- WithBrokerSearchEndpoint + lookupEntity + entityPath ----

func TestWithBrokerSearchEndpoint_wiresHandler(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	b := NewBroker(nil, nil, nil, "", WithBrokerSearchEndpoint(h))
	if b.searchEndpoint == nil {
		t.Fatal("WithBrokerSearchEndpoint did not set searchEndpoint")
	}
}

func TestLookupEntity_nilRegistryDenies(t *testing.T) {
	b := NewBroker(nil, nil, nil, "")
	if ent := b.lookupEntity("anything"); ent != nil {
		t.Error("lookupEntity on nil registry must return nil")
	}
}

func TestLookupEntity_emptyNameDenies(t *testing.T) {
	b := NewBroker(nil, brokerRegistry(), nil, "")
	if ent := b.lookupEntity(""); ent != nil {
		t.Error("lookupEntity('') must return nil")
	}
}

func TestEntityPath_prefixAndNoPrefix(t *testing.T) {
	ent := brokerEntity("items", "items", nil)
	b := NewBroker(nil, brokerRegistry(ent), nil, "")
	if got := b.entityPath(ent); got != "/items" {
		t.Errorf("entityPath (no prefix) = %q", got)
	}
	b2 := NewBroker(nil, brokerRegistry(ent), nil, "/api")
	if got := b2.entityPath(ent); got != "/api/items" {
		t.Errorf("entityPath (/api) = %q", got)
	}
}
func TestMintDelegation_ambientIsCallerless(t *testing.T) {
	b := NewBroker(nil, nil, nil, "")
	handle, release := b.MintDelegation(nil, 1)
	defer release()
	if handle == "" {
		t.Fatal("ambient handle = empty; MintDelegation always returns a random handle")
	}
	// The entry is caller-less: no cookie / authorization stashed.
	b.mu.Lock()
	entry, ok := b.handles[handle]
	b.mu.Unlock()
	if !ok {
		t.Fatalf("ambient handle %q not stashed", handle)
	}
	if entry.cookie != "" || entry.authorization != "" {
		t.Errorf("ambient entry = %+v, want no caller credentials", entry)
	}
}

func TestMintDelegation_withRequestStash(t *testing.T) {
	b := NewBroker(nil, nil, nil, "")
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Cookie", "sid=abc")
	handle, release := b.MintDelegation(req, 2)
	defer release()
	if handle == "" {
		t.Fatal("handle empty for non-nil request")
	}
	// The stashed entry must round-trip the cookie via snapshotRequest.
	b.mu.Lock()
	entry, ok := b.handles[handle]
	b.mu.Unlock()
	if !ok || entry.cookie != "sid=abc" {
		t.Errorf("stashed entry = %+v ok=%v", entry, ok)
	}
}

// ---- dispatchEntity nil-router denial (covers the brokerDeny branch) ----

func TestDispatchEntity_nilRouterDenies(t *testing.T) {
	b := NewBroker(nil, brokerRegistry(brokerEntity("articles", "articles", nil)), nil, "")
	params, _ := json.Marshal(moduleproto.EntityQueryParams{Entity: "articles"})
	_, err := b.dispatchEntity(opQuery, b.lookupEntity("articles"), context.Background(), params)
	if err == nil {
		t.Fatal("dispatchEntity with nil router must deny")
	}
	we, ok := err.(*moduleproto.Error)
	if !ok || we.Code != moduleproto.CodeCapabilityDenied {
		t.Fatalf("nil-router dispatch error = %T %+v, want CodeCapabilityDenied", err, we)
	}
}

// ---- dispatchEntity mutation ops against a fake success router ----

// fakeEnvelopeRouter returns 200 + a JSON envelope for any request, and
// records the method/path it was called with.
func fakeEnvelopeRouter(hit *bool, lastMethod *string, lastPath *string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*hit = true
		*lastMethod = r.Method
		*lastPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"id":1,"ok":true}}`))
	})
}

func TestDispatchEntity_createMapsEnvelope(t *testing.T) {
	var hit bool
	var method, path string
	b := NewBroker(fakeEnvelopeRouter(&hit, &method, &path), brokerRegistry(brokerEntity("articles", "articles", nil)), nil, "")
	params, _ := json.Marshal(moduleproto.EntityMutationParams{
		Entity:  "articles",
		Payload: json.RawMessage(`{"title":"x"}`),
	})
	res, err := b.dispatchEntity(opCreate, b.lookupEntity("articles"), context.Background(), params)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !hit || method != http.MethodPost || path != "/articles" {
		t.Errorf("create not POSTed to /articles: hit=%v method=%q path=%q", hit, method, path)
	}
	mr, ok := res.(moduleproto.EntityMutationResult)
	if !ok || mr.Affected != 1 || string(mr.Rows) == "" {
		t.Errorf("create result = %+v", res)
	}
}

func TestDispatchEntity_updateUsesSingleID(t *testing.T) {
	var hit bool
	var method, path string
	b := NewBroker(fakeEnvelopeRouter(&hit, &method, &path), brokerRegistry(brokerEntity("articles", "articles", nil)), nil, "")
	params, _ := json.Marshal(moduleproto.EntityMutationParams{
		Entity:  "articles",
		IDs:     []string{"42"},
		Payload: json.RawMessage(`{"title":"y"}`),
	})
	if _, err := b.dispatchEntity(opUpdate, b.lookupEntity("articles"), context.Background(), params); err != nil {
		t.Fatalf("update: %v", err)
	}
	if !hit || method != http.MethodPatch || path != "/articles/42" {
		t.Errorf("update not PATCHed to /articles/42: hit=%v method=%q path=%q", hit, method, path)
	}
}

func TestDispatchEntity_deleteUsesSingleID(t *testing.T) {
	var hit bool
	var method, path string
	b := NewBroker(fakeEnvelopeRouter(&hit, &method, &path), brokerRegistry(brokerEntity("articles", "articles", nil)), nil, "")
	params, _ := json.Marshal(moduleproto.EntityMutationParams{
		Entity: "articles",
		IDs:    []string{"7"},
	})
	res, err := b.dispatchEntity(opDelete, b.lookupEntity("articles"), context.Background(), params)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !hit || method != http.MethodDelete || path != "/articles/7" {
		t.Errorf("delete not DELETEd at /articles/7: hit=%v method=%q path=%q", hit, method, path)
	}
	if mr, ok := res.(moduleproto.EntityMutationResult); !ok || mr.Affected != 1 {
		t.Errorf("delete result = %+v", res)
	}
}

func TestDispatchEntity_updateEmptyIDsErrors(t *testing.T) {
	b := NewBroker(fakeEnvelopeRouter(new(bool), new(string), new(string)), brokerRegistry(brokerEntity("articles", "articles", nil)), nil, "")
	params, _ := json.Marshal(moduleproto.EntityMutationParams{Entity: "articles"})
	_, err := b.dispatchEntity(opUpdate, b.lookupEntity("articles"), context.Background(), params)
	if err == nil {
		t.Fatal("update with empty IDs must error")
	}
}

func TestDispatchEntity_queryMapsEnvelope(t *testing.T) {
	var hit bool
	b := NewBroker(fakeEnvelopeRouter(&hit, new(string), new(string)), brokerRegistry(brokerEntity("articles", "articles", nil)), nil, "")
	params, _ := json.Marshal(moduleproto.EntityQueryParams{Entity: "articles"})
	res, err := b.dispatchEntity(opQuery, b.lookupEntity("articles"), context.Background(), params)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if !hit {
		t.Error("query did not reach the router")
	}
	qr, ok := res.(moduleproto.EntityQueryResult)
	if !ok || string(qr.Rows) == "" {
		t.Errorf("query result = %+v", res)
	}
}

// ---- searchHandler success path (covers the WithBrokerSearchEndpoint branch) ----

func TestSearchHandler_succeedsWithEndpoint(t *testing.T) {
	var hit bool
	var qs string
	endpoint := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		qs = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":["r1"],"total":1}`))
	})
	b := NewBroker(nil, nil, nil, "", WithBrokerSearchEndpoint(endpoint))
	view := ModuleGrantView{Name: "m", Grants: []access.Permission{"search:query"}}
	h := b.searchHandler(view)
	params := jsonMarshalRaw(moduleproto.SearchQueryParams{Query: "hello", Limit: 5})
	res, err := callReverse(t, h, params)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !hit {
		t.Error("search endpoint not reached")
	}
	if !strings.Contains(qs, "q=hello") || !strings.Contains(qs, "limit=5") {
		t.Errorf("search query string = %q", qs)
	}
	sr, ok := res.(moduleproto.SearchQueryResult)
	if !ok || sr.Total != 1 || string(sr.Results) == "" {
		t.Errorf("search result = %+v", res)
	}
}

// ---- eventHandler payload-unmarshal error ----

func TestEventHandler_badPayloadErrors(t *testing.T) {
	bus := event.NewEventBus()
	b := NewBroker(nil, nil, bus, "")
	view := ModuleGrantView{Name: "m", Grants: []access.Permission{"alerts:emit"}}
	h := b.eventHandler(view)
	params := jsonMarshalRaw(moduleproto.EventEmitParams{
		Topic:   "alerts.fired",
		Payload: json.RawMessage(`not-json`),
	})
	if _, err := callReverse(t, h, params); err == nil {
		t.Error("event emit with malformed payload must error")
	}
}

// jsonMarshalRaw marshals v and returns the bytes as json.RawMessage.
func jsonMarshalRaw(v any) json.RawMessage {
	raw, _ := json.Marshal(v)
	return raw
}
