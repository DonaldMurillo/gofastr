package mcp

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

const defaultListPageSize = 100

const listCursorPrefix = "v1"

var errInvalidCursor = errors.New("invalid cursor")

type cursorPayload struct {
	Method string `json:"m"`
	Offset int    `json:"o"`
}

func (s *Server) encodeListCursor(method string, offset int) string {
	payload, _ := json.Marshal(cursorPayload{Method: method, Offset: offset})
	body := listCursorPrefix + "." + base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, s.cursorKey())
	mac.Write([]byte(body))
	return body + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

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

func (s *Server) cursorKey() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.cursorSecret) == 0 {
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			panic("mcp: entropy source unavailable: " + err.Error())
		}
		s.cursorSecret = key
	}
	return s.cursorSecret
}

func (s *Server) pageListSize() int {
	s.mu.RLock()
	size := s.listPageSize
	s.mu.RUnlock()
	if size <= 0 {
		return defaultListPageSize
	}
	return size
}

func (s *Server) listOffset(req Request, method string) (int, error) {
	if len(req.Params) == 0 {
		return 0, nil
	}
	var params struct {
		Cursor string `json:"cursor,omitempty"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return 0, errors.New("invalid list params: " + handler.JSONErrorDetail(err).Error())
	}
	if params.Cursor == "" {
		return 0, nil
	}
	return s.decodeListCursor(method, params.Cursor)
}

func requiredArray[T any](xs []T) []T {
	if xs == nil {
		return []T{}
	}
	return xs
}

func pageList[T any](s *Server, method string, items []T, offset int) ([]T, string) {
	if offset >= len(items) {
		return items[:0], ""
	}
	end := offset + min(s.pageListSize(), len(items)-offset)
	if end < len(items) {
		return items[offset:end], s.encodeListCursor(method, end)
	}
	return items[offset:end], ""
}
