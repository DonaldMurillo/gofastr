package embed

import (
	"fmt"
	"strings"
	"testing"
)

// The whole allowlist ships in one frame-ancestors directive on every shell
// response. Past a few hundred customers that directive exceeds the response
// header limits common in proxies, and the failure is not graceful — the
// surface stops loading for every customer at once, including the ones that
// worked yesterday. Boot is the only place to catch it.
func TestOriginListIsCappedAtBoot(t *testing.T) {
	many := make([]string, 0, 300)
	for i := 0; i < 300; i++ {
		many = append(many, fmt.Sprintf("https://customer-%03d.example.com", i))
	}
	_, err := New(Config{
		Surfaces:  []Surface{{Name: "reports", Path: "/reports", Origins: many}},
		BurnStore: NewMemoryBurnStore(),
	})
	if err == nil {
		t.Fatal("New accepted 300 origins — the frame-ancestors directive would be rejected by a proxy")
	}
	// The message has to carry the arithmetic, or the operator cannot tell how
	// far over the line they are.
	for _, want := range []string{"frame-ancestors", "300"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %q, want it to mention %q", err, want)
		}
	}
}

// A realistic allowlist must still boot. A cap that refuses ordinary configs
// would trade one outage for another.
func TestOrdinaryOriginListsStillBoot(t *testing.T) {
	for _, n := range []int{1, 10, 50} {
		origins := make([]string, 0, n)
		for i := 0; i < n; i++ {
			origins = append(origins, fmt.Sprintf("https://customer-%03d.example.com", i))
		}
		if _, err := New(Config{
			Surfaces:  []Surface{{Name: "reports", Path: "/reports", Origins: origins}},
			BurnStore: NewMemoryBurnStore(),
		}); err != nil {
			t.Fatalf("New refused %d origins: %v", n, err)
		}
	}
}
