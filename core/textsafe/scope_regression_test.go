package textsafe

import "testing"

func TestTruncateNegativeCap(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
	if got := Truncate("secret", -1); got != "" {
		t.Fatal(got)
	}
}
