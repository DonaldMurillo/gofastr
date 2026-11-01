package jsonexact

import (
	"encoding/json"
	"testing"
	"time"
)

func TestUnmarshalKeepsBigIntegers(t *testing.T) {
	var v map[string]any
	if err := Unmarshal([]byte(`{"id":9007199254740993,"n":3,"f":1.5,"e":1e300,"x":[9007199254740995]}`), &v); err != nil {
		t.Fatal(err)
	}
	if got, ok := v["id"].(json.Number); !ok || got != "9007199254740993" {
		t.Errorf("id = %#v, want json.Number 9007199254740993", v["id"])
	}
	if got, ok := v["n"].(float64); !ok || got != 3 {
		t.Errorf("n = %#v, want float64 3", v["n"])
	}
	if got, ok := v["f"].(float64); !ok || got != 1.5 {
		t.Errorf("f = %#v, want float64 1.5", v["f"])
	}
	if _, ok := v["e"].(json.Number); !ok {
		t.Errorf("e = %#v, want json.Number (integer-valued past 2^53)", v["e"])
	}
	if got := v["x"].([]any)[0]; got != json.Number("9007199254740995") {
		t.Errorf("x[0] = %#v, want json.Number", got)
	}
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"e":1e300,"f":1.5,"id":9007199254740993,"n":3,"x":[9007199254740995]}`; string(out) != want {
		t.Errorf("round trip = %s, want %s", out, want)
	}
}

func TestUnmarshalKeepsFloatBelowLimit(t *testing.T) {
	var v any
	if err := Unmarshal([]byte(`9007199254740991`), &v); err != nil {
		t.Fatal(err)
	}
	if got, ok := v.(float64); !ok || got != 9007199254740991 {
		t.Errorf("v = %#v, want float64 2^53-1", v)
	}
}

func TestUnmarshalRefusesTrailingData(t *testing.T) {
	var v any
	if err := Unmarshal([]byte(`{} {}`), &v); err == nil {
		t.Error("trailing value accepted")
	}
	if err := Unmarshal([]byte("{} \n"), &v); err != nil {
		t.Errorf("trailing space refused: %v", err)
	}
}

func TestUnmarshalRefusesOutOfRange(t *testing.T) {
	var v any
	if err := Unmarshal([]byte(`1e400`), &v); err == nil {
		t.Error("1e400 accepted")
	}
}

func TestSettleAfterStreamDecode(t *testing.T) {
	var v struct{ Data any }
	if err := json.Unmarshal([]byte(`{"Data":{"id":9007199254740993}}`), &v); err != nil {
		t.Fatal(err)
	}
	// Settle leaves a float64 alone; it acts on json.Number from UseNumber.
	v.Data = map[string]any{"id": json.Number("9007199254740993"), "n": json.Number("2")}
	if err := Settle(&v); err != nil {
		t.Fatal(err)
	}
	m := v.Data.(map[string]any)
	if m["id"] != json.Number("9007199254740993") || m["n"] != float64(2) {
		t.Errorf("settled = %#v", m)
	}
}

type embeddedMeta struct {
	Data any
	hide any
}

type embeddedPtrMeta struct {
	More any
}

// Settle skipped an embedded struct of an unexported type because the
// embed itself is not settable, though encoding/json fills its exported
// fields and those are settable through it. It descends now, value or
// pointer embed, and an unexported leaf is still left alone.
func TestSettleDescendsUnexportedEmbed(t *testing.T) {
	var v struct {
		embeddedMeta
		*embeddedPtrMeta
	}
	v.Data = json.Number("3")
	v.hide = json.Number("4")
	v.embeddedPtrMeta = &embeddedPtrMeta{More: json.Number("5")}
	if err := Settle(&v); err != nil {
		t.Fatal(err)
	}
	if v.Data != float64(3) {
		t.Errorf("promoted Data = %#v, want float64(3)", v.Data)
	}
	if v.More != float64(5) {
		t.Errorf("promoted More through a pointer embed = %#v, want float64(5)", v.More)
	}
	if v.hide != json.Number("4") {
		t.Errorf("unexported leaf changed: %#v", v.hide)
	}
}

// A value that points back into itself through an unexported embedded
// pointer (which Settle now descends into) settles and ends.
func TestSettleEndsOnPointerCycle(t *testing.T) {
	type node struct {
		*node
		V any `json:"v"`
	}
	n := &node{V: json.Number("3")}
	n.node = n
	done := make(chan error, 1)
	go func() { done <- Settle(n) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Settle never returned on a pointer cycle")
	}
	if _, ok := n.V.(float64); !ok {
		t.Fatalf("V = %T, want float64", n.V)
	}
}
