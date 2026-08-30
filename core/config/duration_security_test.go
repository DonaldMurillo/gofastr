package config_test

import (
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/config"
)

// Property: a duration too large for time.Duration is rejected on BOTH
// syntaxes the parser accepts. The plain-integer-seconds path
// (config.go:285-289) multiplies n * 1e9 with no overflow check, so
// TTL=9223372036854775807 (MaxInt64 seconds) wraps to a NEGATIVE
// duration and binds silently — flipping a timeout/TTL/lease negative.
// The Go-suffix syntax already rejects the same magnitude, so today the
// two syntaxes disagree; a hostile env/config value picks the wrapping
// one. Surfaces: overflow via plain seconds (RED), overflow via Go
// suffix (already clean — pinned so a fix keeps it), and a normal value
// (false-positive guard).
func TestLoadDurationOverflowRejected(t *testing.T) {
	type cfg struct {
		TTL time.Duration `config:"TTL"`
	}

	var c cfg
	err := config.Load(&c, config.MapSource{"TTL": "9223372036854775807"})
	if err == nil {
		t.Errorf("SECURITY: [config] TTL=9223372036854775807 bound as %v with no error — plain-seconds path multiplies without an overflow check (config.go:289) and wraps negative", c.TTL)
	}

	var g cfg
	if err := config.Load(&g, config.MapSource{"TTL": "106752d"}); err == nil {
		t.Errorf("[config] 106752d overflows time.Duration and must error like every other Go-duration overflow; bound %v", g.TTL)
	}

	var ok cfg
	if err := config.Load(&ok, config.MapSource{"TTL": "90s"}); err != nil || ok.TTL != 90*time.Second {
		t.Errorf("[config] 90s must keep binding exactly; err=%v ttl=%v", err, ok.TTL)
	}
}
