# 007 — SSE & Chunked Stream Writer

**Phase:** 1 (Core Primitives) | **Tier:** 1 | **Depends on:** nothing

## Goal
Server-Sent Events writer and chunked response writer for streaming HTTP responses. Event types (Message, Error, Done, Custom). Integration with `http.Flusher`. Last-Event-ID support for reconnect. Foundation for AG-UI streaming protocol. Tests with `httptest`.

## Deliverables
- [ ] `SSEWriter` struct wrapping `http.ResponseWriter` with `http.Flusher` integration
- [ ] Event types: `Message`, `Error`, `Done`, `Custom(event, data)`
- [ ] `SSEWriter.Write(event)` — writes properly formatted SSE frames (`event:`, `data:`, `id:`, `retry:`)
- [ ] `SSEWriter.WriteMessage(data)` — convenience for message events
- [ ] `SSEWriter.WriteError(message)` — convenience for error events
- [ ] `SSEWriter.WriteDone()` — sends `[DONE]` sentinel
- [ ] `Last-Event-ID` header parsing from request → resume-from helper
- [ ] `ChunkedWriter` for raw chunked transfer encoding streams
- [ ] Proper content-type headers (`text/event-stream` for SSE)
- [ ] Graceful connection handling: detect client disconnect via `r.Context().Done()`
- [ ] `stream` package at `core/stream/`
- [ ] Tests using `net/http/httptest` — verify frame format, flush behavior, disconnect detection

## Acceptance Criteria
- SSE frames conform to the W3C SSE spec (`event:`, `data:`, `id:` fields, double newline delimiters)
- `http.Flusher` is checked at runtime — panic or error if ResponseWriter doesn't support it
- Last-Event-ID parsed from `Last-Event-ID` header or `last_event_id` query param
- Client disconnect detected via context cancellation without goroutine leak
- All tests pass with `go test ./core/stream/...`
- Zero dependencies outside Go stdlib
