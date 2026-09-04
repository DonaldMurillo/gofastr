package handler

import (
	"strings"
	"testing"
)

type strictEnvelope struct {
	Method string `json:"method"`
	ID     int    `json:"id"`
}

func TestDecodeStrictRefusesDuplicateKey(t *testing.T) {
	var v strictEnvelope
	err := DecodeStrict(strings.NewReader(`{"method":"safe","method":"danger","id":1}`), &v)
	if err == nil || !strings.Contains(err.Error(), "duplicate key") {
		t.Fatalf("duplicate top-level key must be refused, got err=%v v=%+v", err, v)
	}
}

func TestDecodeStrictRefusesCaseFoldedKey(t *testing.T) {
	var v strictEnvelope
	err := DecodeStrict(strings.NewReader(`{"method":"safe","Method":"danger","id":1}`), &v)
	if err == nil {
		t.Fatalf("case-folded top-level key must be refused, got method=%q", v.Method)
	}
}

func TestDecodeStrictRefusesUnknownKeyOnStruct(t *testing.T) {
	var v strictEnvelope
	if err := DecodeStrict(strings.NewReader(`{"method":"x","extra":1}`), &v); err == nil {
		t.Fatal("unknown key on a struct destination must be refused")
	}
}

func TestDecodeStrictAcceptsCleanStruct(t *testing.T) {
	var v strictEnvelope
	if err := DecodeStrict(strings.NewReader(`{"method":"x","id":7}`), &v); err != nil || v.Method != "x" || v.ID != 7 {
		t.Fatalf("clean body must decode, got err=%v v=%+v", err, v)
	}
}

func TestUnmarshalStrictMapRefusesFoldedKeys(t *testing.T) {
	m := map[string]any{}
	err := UnmarshalStrict([]byte(`{"text":"a","Text":"b"}`), &m)
	if err == nil {
		t.Fatalf("two keys folding to one name must be refused on a map, got %v", m)
	}
	m = map[string]any{}
	if err := UnmarshalStrict([]byte(`{"text":"a","text":"b"}`), &m); err == nil {
		t.Fatalf("duplicate key must be refused on a map, got %v", m)
	}
	m = map[string]any{}
	if err := UnmarshalStrict([]byte(`{"text":"a","other":"b"}`), &m); err != nil || m["text"] != "a" {
		t.Fatalf("clean map body must decode, got err=%v m=%v", err, m)
	}
}

func TestCheckTopLevelKeysUsesCallerFold(t *testing.T) {
	snake := func(k string) string { return strings.ReplaceAll(strings.ToLower(k), "_", "") }
	if err := CheckTopLevelKeys([]byte(`{"body_text":"a","bodyText":"b"}`), snake); err == nil {
		t.Fatal("keys that the caller's fold maps onto one column must be refused")
	}
	if err := CheckTopLevelKeys([]byte(`{"body_text":"a","title":"b"}`), snake); err != nil {
		t.Fatalf("distinct keys must pass, got %v", err)
	}
	if err := CheckTopLevelKeys([]byte(`[1,2]`), nil); err != nil {
		t.Fatalf("non-object body is the decoder's business, got %v", err)
	}
}

func TestDecodeStrictNonObjectPassesThrough(t *testing.T) {
	var n int
	if err := DecodeStrict(strings.NewReader(`42`), &n); err != nil || n != 42 {
		t.Fatalf("scalar body must decode unchanged, got err=%v n=%d", err, n)
	}
}

type strictOuter struct {
	Params strictEnvelope `json:"params"`
}

func TestUnmarshalStrictRefusesNestedAmbiguity(t *testing.T) {
	var v strictOuter
	if err := UnmarshalStrict([]byte(`{"params":{"method":"safe","method":"danger","id":1}}`), &v); err == nil {
		t.Fatalf("nested duplicate key must be refused, got method=%q", v.Params.Method)
	}
	v = strictOuter{}
	if err := UnmarshalStrict([]byte(`{"params":{"method":"safe","Method":"danger","id":1}}`), &v); err == nil {
		t.Fatalf("nested case-folded key pair must be refused, got method=%q", v.Params.Method)
	}
	v = strictOuter{}
	if err := UnmarshalStrict([]byte(`{"params":{"method":"x","id":2}}`), &v); err != nil || v.Params.Method != "x" {
		t.Fatalf("clean nested body must decode, got err=%v v=%+v", err, v)
	}
}

func TestCheckObjectKeysWalksArraysAndDepth(t *testing.T) {
	if err := CheckObjectKeys([]byte(`{"a":[{"k":1,"k":2}]}`), nil); err == nil {
		t.Fatal("duplicate inside an array element must be refused")
	}
	if err := CheckObjectKeys([]byte(`{"a":{"b":{"c":{"K":1,"k":2}}}}`), strings.ToLower); err == nil {
		t.Fatal("fold collision three levels down must be refused")
	}
	if err := CheckObjectKeys([]byte(`{"a":[{"k":1},{"k":2}],"b":{"k":3}}`), strings.ToLower); err != nil {
		t.Fatalf("the same key in sibling objects is fine, got %v", err)
	}
	if err := CheckObjectKeys([]byte(`{"a":`), nil); err != nil {
		t.Fatalf("a truncated body is the decoder's business, got %v", err)
	}
}
