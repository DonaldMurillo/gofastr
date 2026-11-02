package handler

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

type pageParams struct {
	Page  int    `query:"page" json:"-"`
	Trace string `header:"X-Trace" json:"-"`
}

type Owner struct {
	OrgID string `path:"org" json:"-"`
}

type listInput struct {
	pageParams
	*Owner
	Q string `query:"q" json:"-"`
}

// Query, header and path tags promoted from an embedded struct bind,
// value or pointer embed, the way encoding/json promotes body keys.
func TestBindFillsEmbeddedTags(t *testing.T) {
	r := httptest.NewRequest("GET", "/orgs/acme/items?page=3&q=x", nil)
	r.Header.Set("X-Trace", "t1")
	r.SetPathValue("org", "acme")
	var in listInput
	if err := Bind(r, &in); err != nil {
		t.Fatal(err)
	}
	if in.Page != 3 || in.Trace != "t1" || in.Q != "x" || in.Owner == nil || in.OrgID != "acme" {
		t.Fatalf("bound %+v owner=%+v", in, in.Owner)
	}
}

// A nil embedded pointer stays nil when none of its tags arrive.
func TestBindLeavesUnusedEmbedNil(t *testing.T) {
	var in listInput
	if err := Bind(httptest.NewRequest("GET", "/items?q=x", nil), &in); err != nil {
		t.Fatal(err)
	}
	if in.Owner != nil {
		t.Fatalf("Owner allocated with no path value: %+v", in.Owner)
	}
}

type selfNode struct {
	*selfNode
	Q string `query:"q"`
}

type hiddenParams struct {
	X string `query:"x"`
}

type withHidden struct {
	*hiddenParams
	Y string `query:"y"`
}

// A struct embedding a pointer to itself binds once and terminates; a
// nil unexported embedded pointer cannot be allocated and is skipped
// rather than panicking.
func TestBindEmbedCycleAndHidden(t *testing.T) {
	var n selfNode
	if err := Bind(httptest.NewRequest("GET", "/?q=a", nil), &n); err != nil || n.Q != "a" {
		t.Fatalf("self embed: %v %+v", err, n)
	}
	var h withHidden
	if err := Bind(httptest.NewRequest("GET", "/?x=1&y=2", nil), &h); err != nil || h.Y != "2" || h.hiddenParams != nil {
		t.Fatalf("hidden embed: %v %+v", err, h)
	}
}

func echoList(_ context.Context, in listInput) (listInput, error) { return in, nil }

// The MCP tool adapter binds the same promoted tags the route does.
func TestToolAdapterBindsEmbeddedTags(t *testing.T) {
	out, err := ToolAdapter(Handler[listInput, listInput](echoList))(context.Background(), map[string]any{
		"page": float64(2), "org": "acme", "q": "y",
	})
	if err != nil {
		t.Fatal(err)
	}
	got := out.(listInput)
	if got.Page != 2 || got.Owner == nil || got.OrgID != "acme" || got.Q != "y" {
		t.Fatalf("bound %+v owner=%+v", got, got.Owner)
	}
}

type innerMeta struct {
	Data any `json:"data"`
}

type metaEmbedReq struct {
	innerMeta
	Name string `json:"name"`
}

// jsonexact.Settle skipped every field it could not set, and an embedded
// struct of an unexported type is one, so a number under an `any` the
// embed promotes stayed a json.Number where a plain field settles to
// float64. Both Bind and the tool adapter decode with UseNumber + Settle.
func TestBindSettlesNumbersInUnexportedEmbed(t *testing.T) {
	r := httptest.NewRequest("POST", "/", strings.NewReader(`{"name":"x","data":3}`))
	r.Header.Set("Content-Type", "application/json")
	var in metaEmbedReq
	if err := Bind(r, &in); err != nil {
		t.Fatal(err)
	}
	if d, ok := in.Data.(float64); !ok || d != 3 {
		t.Errorf("Bind: promoted any field = %#v (%T), want float64(3)", in.Data, in.Data)
	}

	h := Handler[metaEmbedReq, metaEmbedReq](func(_ context.Context, in metaEmbedReq) (metaEmbedReq, error) { return in, nil })
	out, err := ToolAdapter(h)(context.Background(), map[string]any{"data": json.Number("3")})
	if err != nil {
		t.Fatal(err)
	}
	if d, ok := out.(metaEmbedReq).Data.(float64); !ok || d != 3 {
		t.Errorf("ToolAdapter: promoted any field = %#v, want float64(3)", out.(metaEmbedReq).Data)
	}
}
