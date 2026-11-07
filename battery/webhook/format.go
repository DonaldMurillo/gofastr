package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/http/httpguts"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// SignatureFormat is the shape a Manager signs outbound deliveries in, for a
// receiver that already verifies its own scheme. Set it on
// Options.Signature; nil keeps the GoFastr format (X-GoFastr-Signature,
// `t=<unix>,v1=<hex>` over the timestamp, event, delivery id and body).
//
// A receiver that checks `X-Acme-Timestamp: <unix>` and
// `X-Acme-Signature: sha256=<hex HMAC-SHA256(secret, "<unix>.<body>")>` is:
//
//	&webhook.SignatureFormat{
//		Header:          "X-Acme-Signature",
//		TimestampHeader: "X-Acme-Timestamp",
//		Prefix:          "sha256=",
//		Signed:          "{timestamp}.{body}",
//	}
//
// The MAC is always HMAC-SHA256 keyed with Subscriber.Secret, hex-encoded.
type SignatureFormat struct {
	// Header carries the signature value: Prefix followed by the hex MAC.
	// Required.
	Header string
	// TimestampHeader carries the unix-seconds timestamp the signature
	// covers. Empty sends none, which is allowed only when Prefix holds
	// {timestamp}: the receiver has to learn the timestamp somewhere.
	TimestampHeader string
	// EventHeader and DeliveryIDHeader carry the event name and the
	// delivery id. Empty means X-GoFastr-Event and X-GoFastr-Delivery-ID.
	// Both are sent on every delivery whether or not Signed covers them.
	EventHeader      string
	DeliveryIDHeader string
	// Prefix is written before the hex MAC, such as "sha256=" or "v0=". It
	// may hold {timestamp} once (Stripe's "t={timestamp},v1="); no other
	// placeholder.
	Prefix string
	// Signed is the string the HMAC covers. It holds {body} exactly once and
	// {timestamp} at least once; {event} and {delivery_id} are optional.
	// Every other byte is literal. A signed string with no timestamp gives
	// the receiver no replay window, so it is refused.
	//
	// The parts are joined as written. The GoFastr format joins with a
	// newline because event names carry dots; "{event}.{body}" lets one
	// event and body read as another, so sign the event only in a scheme
	// whose separator the event cannot hold.
	Signed string
}

// Signature format placeholders.
const (
	placeTimestamp  = "{timestamp}"
	placeEvent      = "{event}"
	placeDeliveryID = "{delivery_id}"
	placeBody       = "{body}"
)

// defaultSignatureFormat is the GoFastr format, the one SignWithTimestamp and
// VerifyTimestamped speak. The Manager signs through it when
// Options.Signature is nil.
var defaultSignatureFormat = SignatureFormat{
	Header:           SignatureHeader,
	TimestampHeader:  "X-GoFastr-Timestamp",
	EventHeader:      EventHeader,
	DeliveryIDHeader: DeliveryIDHeader,
	Prefix:           "t=" + placeTimestamp + ",v1=",
	Signed:           placeTimestamp + "\n" + placeEvent + "\n" + placeDeliveryID + "\n" + placeBody,
}

// Validate reports the first reason the format cannot sign a delivery. New
// panics on an invalid Options.Signature, so a misconfigured manager fails at
// startup rather than on the first delivery.
func (f SignatureFormat) Validate() error {
	if f.Header == "" {
		return errors.New("webhook: SignatureFormat.Header is required")
	}
	seen := map[string]string{}
	for _, h := range []struct{ field, name string }{
		{"Header", f.Header},
		{"TimestampHeader", f.TimestampHeader},
		{"EventHeader", f.eventHeader()},
		{"DeliveryIDHeader", f.deliveryIDHeader()},
	} {
		if h.name == "" {
			continue
		}
		if !httpguts.ValidHeaderFieldName(h.name) {
			return fmt.Errorf("webhook: SignatureFormat.%s %q is not a valid header name", h.field, textsafe.ScrubControlBytes(h.name))
		}
		canon := http.CanonicalHeaderKey(h.name)
		if reservedAuthHeaders[canon] {
			return fmt.Errorf("webhook: SignatureFormat.%s %q is a header the delivery sets itself", h.field, canon)
		}
		if other, dup := seen[canon]; dup {
			return fmt.Errorf("webhook: SignatureFormat.%s and %s both name %q", other, h.field, canon)
		}
		seen[canon] = h.field
	}
	if !httpguts.ValidHeaderFieldValue(f.Prefix) {
		return errors.New("webhook: SignatureFormat.Prefix holds a control byte; a header value cannot carry it")
	}
	prefixCounts, err := countPlaceholders("Prefix", f.Prefix)
	if err != nil {
		return err
	}
	for name, n := range prefixCounts {
		if name != placeTimestamp {
			return fmt.Errorf("webhook: SignatureFormat.Prefix may hold only %s, not %s", placeTimestamp, name)
		}
		if n > 1 {
			return fmt.Errorf("webhook: SignatureFormat.Prefix holds %s more than once", placeTimestamp)
		}
	}
	counts, err := countPlaceholders("Signed", f.Signed)
	if err != nil {
		return err
	}
	if counts[placeBody] != 1 {
		return fmt.Errorf("webhook: SignatureFormat.Signed must hold %s exactly once", placeBody)
	}
	if counts[placeTimestamp] == 0 {
		return fmt.Errorf("webhook: SignatureFormat.Signed must hold %s: a signature with no timestamp never expires", placeTimestamp)
	}
	if f.TimestampHeader == "" && prefixCounts[placeTimestamp] == 0 {
		return fmt.Errorf("webhook: SignatureFormat needs a TimestampHeader or %s in Prefix, so the receiver can read the signed timestamp", placeTimestamp)
	}
	return nil
}

// countPlaceholders counts each {name} in s, refusing an unknown name or an
// unclosed brace.
func countPlaceholders(field, s string) (map[string]int, error) {
	counts := map[string]int{}
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '{':
			end := strings.IndexByte(s[i:], '}')
			if end < 0 {
				return nil, fmt.Errorf("webhook: SignatureFormat.%s has an unclosed {", field)
			}
			name := s[i : i+end+1]
			switch name {
			case placeTimestamp, placeEvent, placeDeliveryID, placeBody:
				counts[name]++
			default:
				return nil, fmt.Errorf("webhook: SignatureFormat.%s placeholder %q is not one of {timestamp}, {event}, {delivery_id}, {body}", field, textsafe.ScrubControlBytes(name))
			}
			i += end
		case '}':
			return nil, fmt.Errorf("webhook: SignatureFormat.%s has a } with no {", field)
		}
	}
	return counts, nil
}

func (f SignatureFormat) eventHeader() string {
	if f.EventHeader == "" {
		return EventHeader
	}
	return f.EventHeader
}

func (f SignatureFormat) deliveryIDHeader() string {
	if f.DeliveryIDHeader == "" {
		return DeliveryIDHeader
	}
	return f.DeliveryIDHeader
}

// headers is every header name the format writes, canonicalized.
func (f SignatureFormat) headers() []string {
	out := []string{http.CanonicalHeaderKey(f.Header), http.CanonicalHeaderKey(f.eventHeader()), http.CanonicalHeaderKey(f.deliveryIDHeader())}
	if f.TimestampHeader != "" {
		out = append(out, http.CanonicalHeaderKey(f.TimestampHeader))
	}
	return out
}

// Sign returns the signature header value for one delivery: Prefix, with
// {timestamp} filled, then the hex HMAC-SHA256 of Signed. Call it only on a
// format Validate accepts.
func (f SignatureFormat) Sign(secret string, unixTimestamp int64, event, deliveryID string, body []byte) string {
	ts := strconv.FormatInt(unixTimestamp, 10)
	mac := hmac.New(sha256.New, []byte(secret))
	rest := f.Signed
	for rest != "" {
		i := strings.IndexByte(rest, '{')
		if i < 0 {
			mac.Write([]byte(rest))
			break
		}
		mac.Write([]byte(rest[:i]))
		end := strings.IndexByte(rest[i:], '}')
		if end < 0 { // unclosed: Validate refuses it; sign it literally
			mac.Write([]byte(rest[i:]))
			break
		}
		switch rest[i : i+end+1] {
		case placeTimestamp:
			mac.Write([]byte(ts))
		case placeEvent:
			mac.Write([]byte(event))
		case placeDeliveryID:
			mac.Write([]byte(deliveryID))
		case placeBody:
			mac.Write(body)
		}
		rest = rest[i+end+1:]
	}
	return strings.Replace(f.Prefix, placeTimestamp, ts, 1) + hex.EncodeToString(mac.Sum(nil))
}

// setHeaders writes the signature, timestamp, event and delivery id headers
// for one delivery onto h.
func (f SignatureFormat) setHeaders(h http.Header, secret string, unixTimestamp int64, event, deliveryID string, body []byte) {
	h.Set(f.eventHeader(), event)
	h.Set(f.deliveryIDHeader(), deliveryID)
	if f.TimestampHeader != "" {
		h.Set(f.TimestampHeader, strconv.FormatInt(unixTimestamp, 10))
	}
	h.Set(f.Header, f.Sign(secret, unixTimestamp, event, deliveryID, body))
}

// Verify reports whether a request carrying headers h and body is a delivery
// signed in this format with secret, inside tolerance of now. It reads the
// timestamp from the signature value when Prefix holds {timestamp} (and then
// requires TimestampHeader, when set, to carry the same value), else from
// TimestampHeader, and compares the whole signature value with hmac.Equal.
//
// It rejects an invalid format, an empty or whitespace-only secret, a
// non-positive tolerance and a missing header, as VerifyTimestamped does.
func (f SignatureFormat) Verify(secret string, h http.Header, body []byte, tolerance time.Duration) bool {
	if f.Validate() != nil || strings.TrimSpace(secret) == "" || tolerance <= 0 {
		return false
	}
	got := h.Get(f.Header)
	if got == "" {
		return false
	}
	tsRaw, ok := f.timestampFrom(got, h)
	if !ok || !allDigits(tsRaw) {
		return false
	}
	// Digits only, so ts is non-negative and now - ts cannot overflow.
	ts, err := strconv.ParseInt(tsRaw, 10, 64)
	if err != nil {
		return false
	}
	delta := time.Now().Unix() - ts
	if delta < 0 {
		delta = -delta
	}
	if delta > int64(tolerance/time.Second)+1 {
		return false
	}
	want := f.Sign(secret, ts, h.Get(f.eventHeader()), h.Get(f.deliveryIDHeader()), body)
	return hmac.Equal([]byte(got), []byte(want))
}

// timestampFrom reads the signed timestamp's digits.
func (f SignatureFormat) timestampFrom(sig string, h http.Header) (string, bool) {
	before, _, inPrefix := strings.Cut(f.Prefix, placeTimestamp)
	if !inPrefix {
		v := h.Get(f.TimestampHeader)
		return v, v != ""
	}
	if !strings.HasPrefix(sig, before) {
		return "", false
	}
	digits := sig[len(before):]
	n := 0
	for n < len(digits) && n < 19 && digits[n] >= '0' && digits[n] <= '9' {
		n++
	}
	ts := digits[:n]
	if f.TimestampHeader != "" && h.Get(f.TimestampHeader) != ts {
		return "", false
	}
	return ts, true
}

// Verifier is Verify as an InboundVerifier for IngestHandler: a GoFastr app
// receiving deliveries another app signs in this format.
func (f SignatureFormat) Verifier(secret string, tolerance time.Duration) InboundVerifier {
	return func(r *http.Request, body []byte) error {
		if !f.Verify(secret, r.Header, body, tolerance) {
			return errVerifyFailed
		}
		return nil
	}
}

// allDigits reports whether s is one to 19 ASCII digits.
func allDigits(s string) bool {
	if s == "" || len(s) > 19 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
