// Package stream fakes core/stream's SSE writer and websocket conn
// for the unseated arms' fixtures.
package stream

import "net/http"

// SSEWriter writes SSE frames, each under a fresh write deadline.
type SSEWriter struct{ w http.ResponseWriter }

// NewSSEWriter wraps w; the content-type set lives in this package.
func NewSSEWriter(w http.ResponseWriter) *SSEWriter { return &SSEWriter{w: w} }

func (s *SSEWriter) WriteEvent(event, data string) error { return nil }
func (s *SSEWriter) WriteComment(c string) error         { return nil }
func (s *SSEWriter) Flush()                              {}

// WebSocketConn bounds every frame write with WSConfig.WriteTimeout.
type WebSocketConn struct{}

func (c *WebSocketConn) Read() ([]byte, error)      { return nil, nil }
func (c *WebSocketConn) Write(b []byte) error       { return nil }
func (c *WebSocketConn) WriteString(s string) error { return nil }
func (c *WebSocketConn) Close() error               { return nil }

// Upgrade hands back an upgraded conn.
func Upgrade(w http.ResponseWriter, r *http.Request) (*WebSocketConn, error) {
	return &WebSocketConn{}, nil
}
