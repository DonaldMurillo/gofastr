package mcp

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// defaultListPageSize is the page size every paginated list method serves
// (tools/list, resources/list, resources/templates/list, prompts/list).
// The MCP spec leaves the page size to the server: a client pages by
// repeating the request with the previous response's nextCursor until the
// key is absent. 100 keeps every listing a GoFastr app serves today on a
// single page, so pre-pagination clients see the exact old wire shape.
const defaultListPageSize = 100

// listCursorPrefix version-gates the cursor format so a future encoding
// change cannot be misread as the current one.
const listCursorPrefix = "v1"

// errInvalidCursor is the refusal every malformed, tampered or foreign
// cursor gets. Deliberately generic: the cursor is client-supplied, so the
// error must not echo it or explain which check it failed.
var errInvalidCursor = errors.New("invalid cursor")

// cursorPayload is the state a nextCursor carries between pages: the list
// method that minted it and the resume offset into that method's
// POST-FILTER, sorted listing. Nothing else — the total count and the
// page size are deliberately absent, so a cursor cannot be turned into an
// oracle for how many items exist.
type cursorPayload struct {
	Method string `json:"m"`
	Offset int    `json:"o"`
}

// Cursor encoding: "v1.<base64url(payload JSON)>.<base64url(HMAC)>". The
// HMAC is keyed by a per-server random secret and covers the version and
// payload bytes, which makes the cursor UNFORGEABLE: a client cannot mint
// a cursor for an offset it was never handed (negative, out-of-range or
// mid-set), cannot move one between list methods, and cannot alter the
// offset inside a legitimate one. The payload is authenticated, not
// encrypted — it reveals only the resume point the client itself already
// reached. The secret is per process: a restart invalidates outstanding
// cursors and the client restarts the walk from page 1, which the spec's
// repeat-until-absent loop does anyway.

// encodeListCursor mints the opaque cursor for resuming a paginated list
// at offset. Only pageList calls this, so minted offsets are always the
// end of a full, in-range page.
func (s *Server) encodeListCursor(method string, offset int) string {
	payload, _ := json.Marshal(cursorPayload{Method: method, Offset: offset})
	body := listCursorPrefix + "." + base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, s.cursorKey())
	mac.Write([]byte(body))
	return body + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// decodeListCursor verifies and decodes a client-supplied cursor for
// method. Any structural defect, signature mismatch, foreign method name
// or negative offset is errInvalidCursor; the caller reports it as a
// JSON-RPC invalid-params error, never a panic and never a silent reset
// to page 1.
func (s *Server) decodeListCursor(method, cursor string) (int, error) {
	parts := strings.Split(cursor, ".")
	if len(parts) != 3 || parts[0] != listCursorPrefix {
		return 0, errInvalidCursor
	}
	body := parts[0] + "." + parts[1]
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return 0, errInvalidCursor
	}
	mac, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return 0, errInvalidCursor
	}
	want := hmac.New(sha256.New, s.cursorKey())
	want.Write([]byte(body))
	if !hmac.Equal(mac, want.Sum(nil)) {
		return 0, errInvalidCursor
	}
	var p cursorPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return 0, errInvalidCursor
	}
	if p.Method != method || p.Offset < 0 {
		return 0, errInvalidCursor
	}
	return p.Offset, nil
}

// cursorKey returns the per-server HMAC secret for list cursors,
// generating it on first use. NewServer callers get it eagerly; the lazy
// path exists so a zero-value Server still mints unforgeable cursors.
func (s *Server) cursorKey() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.cursorSecret) == 0 {
		key := make([]byte, 32)
		// crypto/rand.Read is documented never to fail on the platforms
		// Go supports (Go 1.24+); the error branch is compile-only.
		if _, err := rand.Read(key); err != nil {
			panic("mcp: entropy source unavailable: " + err.Error())
		}
		s.cursorSecret = key
	}
	return s.cursorSecret
}

// pageListSize returns the effective page size, substituting the default
// for an unset (zero-value Server) or non-positive override.
func (s *Server) pageListSize() int {
	s.mu.RLock()
	size := s.listPageSize
	s.mu.RUnlock()
	if size <= 0 {
		return defaultListPageSize
	}
	return size
}

// listOffset extracts the optional cursor param shared by every
// paginated list request and resolves it to a resume offset. A request
// with no cursor (or an empty one) starts at 0; a malformed params
// object or an invalid cursor is an error the caller surfaces as
// invalid-params.
func (s *Server) listOffset(req Request, method string) (int, error) {
	if len(req.Params) == 0 {
		return 0, nil
	}
	var params struct {
		Cursor string `json:"cursor,omitempty"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return 0, fmt.Errorf("invalid list params: %w", err)
	}
	if params.Cursor == "" {
		return 0, nil
	}
	return s.decodeListCursor(method, params.Cursor)
}

// pageList cuts one page from items at offset and returns it with the
// next cursor ("" on the last page). offset must already be validated
// (listOffset rejects negative and forged values). An offset at or past
// the end is a STALE cursor — items vanished between pages — and
// terminates the walk: empty page, no nextCursor, no error. items must
// be non-nil (the listing builders guarantee it) so an empty page
// serializes as [] rather than null.
func pageList[T any](s *Server, method string, items []T, offset int) ([]T, string) {
	if offset >= len(items) {
		return items[:0], ""
	}
	end := offset + s.pageListSize()
	if end > len(items) {
		end = len(items)
	}
	if end < len(items) {
		return items[offset:end], s.encodeListCursor(method, end)
	}
	return items[offset:end], ""
}
