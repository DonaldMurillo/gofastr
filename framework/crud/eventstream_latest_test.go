package crud

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/event"
)

type pausedStreamWriter struct {
	header    http.Header
	text      syncBuffer
	ready     chan struct{}
	paused    chan struct{}
	resume    chan struct{}
	readyOnce sync.Once
	pauseOnce sync.Once
}

func (writer *pausedStreamWriter) Header() http.Header { return writer.header }
func (writer *pausedStreamWriter) WriteHeader(int)     {}
func (writer *pausedStreamWriter) Flush()              {}
func (writer *pausedStreamWriter) Write(data []byte) (int, error) {
	if bytes.Contains(data, []byte(": subscribed")) {
		writer.readyOnce.Do(func() { close(writer.ready) })
	}
	if bytes.Contains(data, []byte("data:")) {
		writer.pauseOnce.Do(func() { close(writer.paused); <-writer.resume })
	}
	return writer.text.Write(data)
}

func TestEventStreamKeepsLatest(t *testing.T) {
	ch := seatCapFeedHandler(t)
	writer := &pausedStreamWriter{header: http.Header{}, ready: make(chan struct{}), paused: make(chan struct{}), resume: make(chan struct{})}
	req := makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/cap_feed", UserID: "alice"})
	ctx, cancel := context.WithCancel(req.Context())
	done := make(chan struct{})
	var resumeOnce sync.Once
	resume := func() { resumeOnce.Do(func() { close(writer.resume) }) }
	t.Cleanup(func() { cancel(); resume(); <-done })
	go func() { defer close(done); ch.EventStream()(writer, req.WithContext(ctx)) }()
	wait := func(signal <-chan struct{}) {
		t.Helper()
		select {
		case <-signal:
		case <-time.After(3 * time.Second):
			t.Fatal("stream did not reach checkpoint")
		}
	}
	wait(writer.ready)
	emit := func(index int) {
		t.Helper()
		payload := ch.eventData(req.Context(), map[string]any{"id": fmt.Sprint(index), "user_id": "alice", "title": "entry"})
		if err := ch.Events.Emit(context.Background(), event.Event{Type: event.EntityCreated, Data: payload}); err != nil {
			t.Fatal(err)
		}
	}
	emit(0)
	wait(writer.paused)
	for index := 1; index <= 40; index++ {
		emit(index)
	}
	resume()
	seatWaitUntil(t, func() bool { return strings.Contains(writer.text.String(), `"id":"40"`) })
	if strings.Contains(writer.text.String(), `"id":"1"`) {
		t.Error("full stream retained the oldest queued event instead of the latest 32")
	}
}
