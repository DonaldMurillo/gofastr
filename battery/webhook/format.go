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

// SignatureFormat is the shape a Manager signs deliveries in, for a receiver
// that verifies its own scheme.
type SignatureFormat struct {
	// Header carries the signature value: Prefix followed by the hex MAC.
	// Required.
	Header string
	// TimestampHeader carries the signed unix timestamp; empty is allowed only
	// when Prefix holds {timestamp}.
	TimestampHeader string
	// EventHeader and DeliveryIDHeader carry the event name and delivery id;
	// empty means the GoFastr headers.
	EventHeader      string
	DeliveryIDHeader string
	// Prefix is written before the hex MAC and may hold {timestamp} once.
	Prefix string
	// Signed is the string the HMAC covers: {body} once, {timestamp} at least
	// once, {event} and {delivery_id} optional.
	Signed string
}

// Signature format placeholders.
const (
	placeTimestamp  = "{timestamp}"
	placeEvent      = "{event}"
	placeDeliveryID = "{delivery_id}"
	placeBody       = "{body}"
)

var defaultSignatureFormat = SignatureFormat{
	Header:           SignatureHeader,
	TimestampHeader:  "X-GoFastr-Timestamp",
	EventHeader:      EventHeader,
	DeliveryIDHeader: DeliveryIDHeader,
	Prefix:           "t=" + placeTimestamp + ",v1=",
	Signed:           placeTimestamp + "\n" + placeEvent + "\n" + placeDeliveryID + "\n" + placeBody,
}

// Validate reports the first reason the format cannot sign a delivery.
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

func (f SignatureFormat) headers() []string {
	out := []string{http.CanonicalHeaderKey(f.Header), http.CanonicalHeaderKey(f.eventHeader()), http.CanonicalHeaderKey(f.deliveryIDHeader())}
	if f.TimestampHeader != "" {
		out = append(out, http.CanonicalHeaderKey(f.TimestampHeader))
	}
	return out
}

// Sign returns the signature header value for one delivery; call it only on a
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
		if end < 0 {
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

func (f SignatureFormat) setHeaders(h http.Header, secret string, unixTimestamp int64, event, deliveryID string, body []byte) {
	h.Set(f.eventHeader(), event)
	h.Set(f.deliveryIDHeader(), deliveryID)
	if f.TimestampHeader != "" {
		h.Set(f.TimestampHeader, strconv.FormatInt(unixTimestamp, 10))
	}
	h.Set(f.Header, f.Sign(secret, unixTimestamp, event, deliveryID, body))
}

// Verify reports whether a request carrying headers h and body is a
// delivery signed in this format with secret, inside tolerance of now.
// It reads the timestamp from the signature value when Prefix holds
// {timestamp} (and then requires TimestampHeader, when set, to carry the
// same value), else from TimestampHeader. It compares the entire
// signature value with hmac.Equal.
func (f SignatureFormat) Verify(secret string, h http.Header, body []byte, tolerance time.Duration) bool {
	return f.verify(secret, h, body, tolerance, false)
}

func (f SignatureFormat) verify(secret string, h http.Header, body []byte, tolerance time.Duration, gofastrFields bool) bool {
	if f.Validate() != nil || strings.TrimSpace(secret) == "" || tolerance <= 0 {
		return false
	}
	got := h.Get(f.Header)
	if got == "" {
		return false
	}
	var tsRaw string
	if gofastrFields {
		tsRaw, got = gofastrSignatureFields(got)
	} else {
		tsRaw = f.timestampFrom(got, h)
		if !allDigits(tsRaw) {
			return false
		}
	}
	ts, err := strconv.ParseInt(tsRaw, 10, 64)
	// Reject negative timestamps before subtracting to avoid overflow.
	if err != nil || ts < 0 {
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
	if gofastrFields {
		want = want[len(want)-hex.EncodedLen(sha256.Size):]
	}
	return hmac.Equal([]byte(got), []byte(want))
}

// timestampFrom reads the timestamp from the exact custom wire format.
func (f SignatureFormat) timestampFrom(sig string, h http.Header) string {
	before, after, inPrefix := strings.Cut(f.Prefix, placeTimestamp)
	if !inPrefix {
		return h.Get(f.TimestampHeader)
	}
	// Locate the timestamp relative to the end of the fixed-length MAC.
	// The suffix can be empty or occur in the timestamp itself.
	end := len(sig) - hex.EncodedLen(sha256.Size) - len(after)
	if !strings.HasPrefix(sig, before) || end <= len(before) || sig[end:end+len(after)] != after {
		return ""
	}
	ts := sig[len(before):end]
	if f.TimestampHeader != "" && h.Get(f.TimestampHeader) != ts {
		return ""
	}
	return ts
}

// gofastrSignatureFields reads the built-in comma-list wire format.
// Unknown fields can occur anywhere, but repeated t or v1 fields make
// the signature ambiguous for a reader that takes the first or last.
func gofastrSignatureFields(sig string) (timestamp, mac string) {
	var timestampSeen, macSeen bool
	for part := range strings.SplitSeq(sig, ",") {
		key, value, found := strings.Cut(strings.TrimSpace(part), "=")
		if !found {
			continue
		}
		switch key {
		case "t":
			if timestampSeen {
				return "", ""
			}
			timestamp, timestampSeen = value, true
		case "v1":
			if macSeen {
				return "", ""
			}
			mac, macSeen = value, true
		}
	}
	return timestamp, mac
}

// Verifier is Verify as an InboundVerifier for IngestHandler.
func (f SignatureFormat) Verifier(secret string, tolerance time.Duration) InboundVerifier {
	return func(r *http.Request, body []byte) error {
		if !f.Verify(secret, r.Header, body, tolerance) {
			return errVerifyFailed
		}
		return nil
	}
}

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
