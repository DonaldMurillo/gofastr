package entity

import (
	"encoding/json"
	"testing"
)

// A form item with two unknown keys is refused naming the same one on
// every decode: UnmarshalJSON ranged the decoded object, so the refusal
// named whichever key the iteration reached first.
func TestFormItemUnknownKeyRefusalIsDeterministic(t *testing.T) {
	data := []byte(`{"k1":1,"k2":2,"k3":3,"k4":4}`)
	var fi FormItem
	first := json.Unmarshal(data, &fi)
	if first == nil {
		t.Fatal("unknown keys accepted")
	}
	for range 64 {
		var again FormItem
		if err := json.Unmarshal(data, &again); err == nil || err.Error() != first.Error() {
			t.Fatalf("refusal changed between decodes: %q then %v", first, err)
		}
	}
}
