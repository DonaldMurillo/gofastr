// Package g holds the 2026-11 bug-bash fixtures: shape D (a caller's
// seconds multiplied into a Duration), shape E (limit+1 handed to a
// reader or make) and shape F (page offset math), each reduced from its
// pre-fix site, beside the fix spelling and the near-misses.
package g

import (
	"encoding/json"
	"io"
	"math"
	"math/bits"
	"net/http"
	"strconv"
	"time"
)

// --- Shape D: 8b19113f2 (ttl_seconds) and ce72591bd #7 (expires_in) ---

type createTokenRequest struct {
	TTLSeconds int64 `json:"ttl_seconds"`
}

type tokenSpec struct{ TTL time.Duration }

// Pre-fix battery/auth apitoken_routes.go: only the negative refused.
func createToken(r *http.Request) tokenSpec {
	var body createTokenRequest
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.TTLSeconds < 0 {
		return tokenSpec{}
	}
	return tokenSpec{TTL: time.Duration(body.TTLSeconds) * time.Second} // want `time.Duration\(body.TTLSeconds\) \* time.Second with no upper bound`
}

const maxTokenTTLSeconds = int64(math.MaxInt64 / int64(time.Second))

// The 8b19113f2 fix: refuse past max before the multiply.
func createTokenFixed(body createTokenRequest) tokenSpec {
	if body.TTLSeconds < 0 || body.TTLSeconds > maxTokenTTLSeconds {
		return tokenSpec{}
	}
	return tokenSpec{TTL: time.Duration(body.TTLSeconds) * time.Second}
}

type tokenResponse struct {
	ExpiresIn int64 `json:"expires_in"`
}

// Pre-fix battery/auth oauth2.go exchange (four sites).
func exchange(body tokenResponse) time.Time {
	return time.Now().Add(time.Duration(body.ExpiresIn) * time.Second) // want `time.Duration\(body.ExpiresIn\) \* time.Second with no upper bound`
}

const maxTokenLifetime = 10 * 365 * 24 * time.Hour

// The ce72591bd fix: tokenExpiry's switch bounds before the multiply.
func tokenExpiry(expiresIn int64) time.Time {
	switch {
	case expiresIn > int64(maxTokenLifetime/time.Second):
		return time.Now().Add(maxTokenLifetime)
	case expiresIn > 0:
		return time.Now().Add(time.Duration(expiresIn) * time.Second)
	}
	return time.Time{}
}

// A clamp AFTER the multiply clamps the wrapped value: still fires.
func clampAfter(secs int) time.Duration {
	d := time.Duration(secs) * time.Minute // want `time.Duration\(secs\) \* time.Minute with no upper bound`
	if d > maxTokenLifetime {
		d = maxTokenLifetime
	}
	return d
}

// A bound on the raw seconds written AFTER the multiply is too late:
// the product already wrapped.
func boundAfter(secs int) time.Duration {
	d := time.Duration(secs) * time.Second // want `time.Duration\(secs\) \* time.Second with no upper bound`
	if secs > 3600 {
		d = time.Hour
	}
	return d
}

// A strconv result, reversed operand order.
func fromQuery(r *http.Request) time.Duration {
	n, err := strconv.Atoi(r.URL.Query().Get("wait"))
	if err != nil {
		return 0
	}
	return time.Second * time.Duration(n) // want `time.Duration\(n\) \* time.Second with no upper bound`
}

// A map[string]any extraction.
func fromClaims(claims map[string]any) time.Duration {
	f, _ := claims["ttl"].(float64)
	return time.Duration(int64(f)) * time.Hour // want `time.Duration\(f\) \* time.Hour with no upper bound`
}

// Quiet: constants and package-level values are the author's.
var pollSeconds = 30

func quietSources() time.Duration {
	const n = 5
	local := 7
	return time.Duration(n)*time.Second + time.Duration(pollSeconds)*time.Second + time.Duration(local)*time.Second
}

// Quiet: an enclosing bound against a constant.
func enclosed(secs int) time.Duration {
	if secs <= 3600 {
		return time.Duration(secs) * time.Second
	}
	return time.Hour
}

// Near-miss: a sign check bounds nothing above.
func signOnly(secs int) time.Duration {
	if secs <= 0 {
		return 0
	}
	return time.Duration(secs) * time.Second // want `with no upper bound`
}

// Near-miss: Nanosecond cannot overflow, and a float is not an int.
func nanos(n int64, f float64) time.Duration {
	return time.Duration(n)*time.Nanosecond + time.Duration(f*float64(time.Second))
}

// --- Shape E: c0d6e99e0 (limit+1 at MaxInt64) and framework/image ---

// Pre-fix core/middleware readBodyLimit.
func readBodyLimit(r *http.Request, limit int64) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r.Body, limit+1)) // want `limit \+ 1 to io.LimitReader with no upper bound on limit`
}

// The c0d6e99e0 fix spelling.
func sentinelLimit(limit int64) int64 {
	if limit >= math.MaxInt64 {
		return limit
	}
	return limit + 1
}

func readBodyFixed(r *http.Request, limit int64) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r.Body, sentinelLimit(limit)))
}

type Config struct{ MaxSourceBytes int64 }

const DefaultMaxSourceBytes = 64 << 20

// Pre-fix framework/image DecodeWithConfig: a sign check, not a cap.
func decode(r io.Reader, cfg Config) ([]byte, error) {
	limit := cfg.MaxSourceBytes
	if limit <= 0 {
		limit = DefaultMaxSourceBytes
	}
	return io.ReadAll(io.LimitReader(r, limit+1)) // want `limit \+ 1 to io.LimitReader with no upper bound on limit`
}

// A receiver's configured cap (an option sets it from a caller value).
type store struct{ maxBody int64 }

func WithMaxBody(n int64) func(*store) { return func(s *store) { s.maxBody = n } }

func (s *store) read(w io.Writer, r io.Reader) {
	_, _ = io.CopyN(w, r, s.maxBody+1) // want `s.maxBody \+ 1 to io.CopyN with no upper bound`
	// make is not a sink: an unbounded caller cap panics make with or
	// without the +1, so the wrap adds nothing.
	_ = make([]byte, 0, s.maxBody+1)
}

// Quiet: a receiver field the package only sets to a constant
// (core/webbotauth's maxBody) is the author's cap.
type directory struct{ maxBody int64 }

func newDirectory() *directory { return &directory{maxBody: 1 << 20} }

func (d *directory) fetch(r io.Reader) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r, d.maxBody+1))
}

// Quiet: an unexported helper every caller hands a constant
// (desktopcmd readLinuxInstallFile).
func readCapped(r io.Reader, limit int64) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r, limit+1))
}

func readManifest(r io.Reader) ([]byte, error) { return readCapped(r, 512<<10) }
func readIcon(r io.Reader) ([]byte, error)     { return readCapped(r, 16<<20) }

// Fires: the same helper once a caller hands it a variable.
func readCappedVar(r io.Reader, limit int64) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r, limit+1)) // want `limit \+ 1 to io.LimitReader with no upper bound on limit`
}

func readAny(r io.Reader, n int64) ([]byte, error) { return readCappedVar(r, n) }

// Quiet: a constant cap is compile-time checked.
const maxFileBytes = 10 << 20

func readConst(r io.Reader) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r, maxFileBytes+1))
}

// Quiet: the cap is bounded above before the +1.
func readBounded(r io.Reader, limit int64) ([]byte, error) {
	if limit > maxFileBytes {
		limit = maxFileBytes
	}
	return io.ReadAll(io.LimitReader(r, limit+1))
}

// Near-miss: a local computed from a constant is not caller-reachable,
// and a +1 that is not the cap argument is not this shape.
func readLocal(r io.Reader, n int64) ([]byte, error) {
	cap := int64(maxFileBytes)
	_ = make([]int, int(n)) // no +k
	return io.ReadAll(io.LimitReader(r, cap+1))
}

// --- Shape F: b973561c4 (?page= offset overflow) ---

// Pre-fix: the skip reckoned by hand.
func offsetFor(page, limit int) int {
	if page < 1 {
		page = 1
	}
	return (page - 1) * limit // want `offset \(page - 1\) \* limit with no overflow pre-check`
}

func listQuery(r *http.Request, limit int) map[string]any {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	offset := page * limit // want `offset page \* limit with no overflow pre-check`
	return map[string]any{"offset": offset}
}

// pagination.OffsetForPage: the division pre-check.
func OffsetForPage(page, limit int) int {
	if page < 2 || limit < 1 {
		return 0
	}
	if page-1 > math.MaxInt/limit {
		return 0
	}
	return (page - 1) * limit
}

// math/bits.Mul is the other fix spelling.
func offsetBits(page, limit uint) uint {
	hi, lo := bits.Mul(page-1, limit)
	if hi != 0 {
		return 0
	}
	return lo
}

// Quiet: battery/admin audit clamps the page with min before it scales.
func auditPage(r *http.Request, pages, limit int) int {
	if limit > 200 {
		limit = 200
	}
	page := 1
	if n, err := strconv.Atoi(r.URL.Query().Get("p")); err == nil && n > 1 {
		page = n
	}
	page = min(page, pages)
	return (page - 1) * limit
}

// Quiet: an operand the caller cannot reach is the author's, and the
// one it can is bounded (framework/entityui SearchRecords).
type listOptions struct{ Limit, Offset int }

func searchOffsets(limit int) []listOptions {
	const searchPages = 3
	limit = min(max(limit, 1), 20)
	var out []listOptions
	for page := range searchPages {
		out = append(out, listOptions{Limit: limit, Offset: page * limit})
	}
	return out
}

// Fires: the same read with the clamp removed.
func searchOffsetsUnclamped(limit int) []listOptions {
	var out []listOptions
	for page := range 3 {
		out = append(out, listOptions{Limit: limit, Offset: page * limit}) // want `offset page \* limit with no overflow pre-check`
	}
	return out
}

// Quiet: both operands bounded above.
func offsetBounded(page, limit int) int {
	if page > 1000 || limit > 100 {
		return 0
	}
	return (page - 1) * limit
}

// Near-miss: a product that is not an offset (an area) and has no -1.
func area(w, h int) int {
	return w * h
}

// Near-miss: constants and locals of constants.
func offsetConst() int {
	per := 20
	return (3 - 1) * per
}
