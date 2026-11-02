package webbotauth

import (
	"net/http"
	"testing"
	"time"
)

// responseTTL multiplied the directory's max-age seconds into a Duration
// before clamping, so a max-age past MaxInt64 nanoseconds wrapped first
// and the clamp saw the wrapped value: max-age=18446744074 (about 584
// years) wrapped to 290ms and cached the directory for the one-minute
// floor instead of the 24-hour ceiling. The seconds are now bounded
// before the multiply.
func TestResponseTTLHugeMaxAgeClampsToCeiling(t *testing.T) {
	now := time.Now()
	for _, v := range []string{"18446744074", "9223372037", "99999999999999"} {
		h := http.Header{}
		h.Set("Cache-Control", "public, max-age="+v)
		d, ok := responseTTL(h, now)
		if !ok || d != maxPositiveTTL {
			t.Errorf("max-age=%s: ttl = %s (ok %v), want the %s ceiling", v, d, ok, maxPositiveTTL)
		}
	}
}
