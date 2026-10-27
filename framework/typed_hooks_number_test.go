package framework

import (
	"fmt"
	"testing"
)

// A payload that is not a map took a plain json.Unmarshal, so a number
// under an interface in the hook's type came back as 9007199254740992.
func TestHookNonMapKeepsBigIntegers(t *testing.T) {
	var dest []any
	if err := unmarshalHookPayload([]any{int64(9007199254740993), 2}, &dest); err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(dest[0]); got != "9007199254740993" {
		t.Errorf("dest[0] = %s, want 9007199254740993", got)
	}
	if got, ok := dest[1].(float64); !ok || got != 2 {
		t.Errorf("dest[1] = %#v, want float64 2", dest[1])
	}
}
