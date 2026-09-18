package local

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// The download bridge. A handler answering an RPC writes records into
// the browser through one response header, X-Gofastr-Local:
//
//	{"app":"<app>","ops":[{"c":"<coll>","k":"<key>","v":<json>},
//	                      {"c":"<coll>","k":"<key>","d":true},
//	                      {"clear":true}]}
//
// rpc.js hands the header to the local-store module's response hook,
// which applies each op through the same put/delete a page write
// uses, so the caps hold and subscribers hear it. Calls accumulate on
// one response the way ui.AddToast does. The header is pure ASCII
// (every non-ASCII rune is \u-escaped): a header value is bytes, and
// the browser reads it as Latin-1.

// ResponseHeader is the header the download bridge rides.
const ResponseHeader = "X-Gofastr-Local"

// ResponseHeaderMaxBytes bounds the accumulated header. A response
// that needs more is pushing a dataset, which is what a body is for.
const ResponseHeaderMaxBytes = 16 << 10

type responseOp struct {
	C     string          `json:"c,omitempty"`
	K     string          `json:"k,omitempty"`
	V     json.RawMessage `json:"v,omitempty"`
	D     bool            `json:"d,omitempty"`
	Clear bool            `json:"clear,omitempty"`
}

type responseMsg struct {
	App string       `json:"app"`
	Ops []responseOp `json:"ops"`
}

// Put writes record key of c into the browser on this response.
// It errors on an invalid key, a value that does not encode or is
// over the collection's record cap, a header already carrying another
// store, or a header over ResponseHeaderMaxBytes.
func Put[T any](w http.ResponseWriter, c *Collection[T], key string, value T) error {
	if !validRecordKey(key) {
		return fmt.Errorf("local: invalid key %q", key)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("local: record %q/%q does not encode: %w", c.def.name, key, err)
	}
	if len(raw) > c.def.maxRecord {
		return fmt.Errorf("%w: record %q/%q is %d bytes, cap %d", ErrTooLarge, c.def.name, key, len(raw), c.def.maxRecord)
	}
	return appendOp(w, c.def.store, responseOp{C: c.def.name, K: key, V: raw})
}

// Delete removes record key of c from the browser on this response.
func Delete[T any](w http.ResponseWriter, c *Collection[T], key string) error {
	if !validRecordKey(key) {
		return fmt.Errorf("local: invalid key %q", key)
	}
	return appendOp(w, c.def.store, responseOp{C: c.def.name, K: key, D: true})
}

// Clear removes every record of every collection of s from the
// browser on this response: the RPC half of logout. A logout that is
// a full navigation (a form POST answered with a redirect) never
// reaches rpc.js; use ClearOnNextLoad there.
func Clear(w http.ResponseWriter, s *Store) error {
	return appendOp(w, s, responseOp{Clear: true})
}

func appendOp(w http.ResponseWriter, s *Store, op responseOp) error {
	msg := responseMsg{App: s.app, Ops: []responseOp{}}
	if cur := w.Header().Get(ResponseHeader); cur != "" {
		if err := json.Unmarshal([]byte(cur), &msg); err != nil {
			return fmt.Errorf("local: %s already carries something that is not this bridge's message", ResponseHeader)
		}
		if msg.App != s.app {
			return fmt.Errorf("local: %s already carries store %q; one store per response", ResponseHeader, msg.App)
		}
	}
	msg.Ops = append(msg.Ops, op)
	enc, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	text := asciiJSON(enc)
	if len(text) > ResponseHeaderMaxBytes {
		return fmt.Errorf("%w: %s would be %d bytes, cap %d", ErrTooLarge, ResponseHeader, len(text), ResponseHeaderMaxBytes)
	}
	w.Header().Set(ResponseHeader, text)
	return nil
}

// asciiJSON rewrites every non-ASCII rune of valid JSON as a \u escape
// (a surrogate pair above the BMP), and DEL (0x7F) with it: JSON
// structure is ASCII, so the runes only occur inside strings, where the
// escape is legal. encoding/json escapes the C0 bytes but not DEL, and
// a raw DEL in a header value breaks the connection on HTTP/1 and makes
// the HTTP/2 writer drop the whole header, every op with it. The same
// gap toast.go closes for its header. The result is printable ASCII.
func asciiJSON(b []byte) string {
	var sb strings.Builder
	sb.Grow(len(b))
	for i := 0; i < len(b); {
		r, size := utf8.DecodeRune(b[i:])
		i += size
		if r < utf8.RuneSelf && r != 0x7F {
			sb.WriteByte(byte(r))
			continue
		}
		if r > 0xFFFF {
			hi, lo := utf16.EncodeRune(r)
			writeU(&sb, hi)
			writeU(&sb, lo)
			continue
		}
		writeU(&sb, r)
	}
	return sb.String()
}

func writeU(sb *strings.Builder, r rune) {
	sb.WriteString(`\u`)
	h := strconv.FormatInt(int64(r), 16)
	sb.WriteString(strings.Repeat("0", 4-len(h)))
	sb.WriteString(h)
}

// ClearOnNextLoad plants the cookie gofastr.local.clear.<app> that the
// local-store module honours on the next page that loads it: it clears
// every record of s, then drops the cookie, and only if the clear
// succeeded. The channel for a logout that is a full navigation.
//
// The module honours the bit at load over every app the manifest
// declares, not only the ones a marker on that page names. It still
// needs the module to load at all, so the bit lives a day rather than
// five minutes: a logout must survive the user landing on a marker-free
// page (a login screen, a marketing page) and coming back later.
//
// The cookie must be readable by script, so it is not HttpOnly; it
// carries a bit, not a value. Secure follows the request's scheme so a
// plain http development origin still stores it.
func ClearOnNextLoad(w http.ResponseWriter, r *http.Request, s *Store) {
	//gofastr:allow(GOFASTR1404) a one-bit flag the browser's own script must read, so HttpOnly would defeat it; Secure follows the request scheme
	http.SetCookie(w, &http.Cookie{
		Name:     "gofastr.local.clear." + s.app,
		Value:    "1",
		Path:     "/",
		MaxAge:   86400,
		SameSite: http.SameSiteLaxMode,
		Secure:   r != nil && (r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")),
		HttpOnly: false,
	})
}
