package web

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/stream"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
)

type publishOnFlush struct {
	http.ResponseWriter
	once    sync.Once
	publish func()
}

func (p *publishOnFlush) Flush() {
	p.ResponseWriter.(http.Flusher).Flush()
	p.once.Do(p.publish)
}

func (p *publishOnFlush) Unwrap() http.ResponseWriter { return p.ResponseWriter }

func TestEventsSeesAnEventPublishedAsTheStreamOpens(t *testing.T) {
	s := newWiredServer(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.handleSSE(&publishOnFlush{ResponseWriter: w, publish: func() {
			_, _ = s.bus.Publish(control.TextDelta{Text: "first"}, "")
		}}, r)
	}))
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		if strings.Contains(sc.Text(), "first") {
			return
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	t.Fatal("an event published the moment the stream opened never reached the browser")
}

func TestEventsRefusesPastTheSeatCap(t *testing.T) {
	s := newWiredServer(t)
	s.seats = control.NewSeatTable()
	for range control.DefaultStreamSeats {
		if _, ok := s.seats.AcquireSeat("", 0, stream.SeatOverflowRefuse); !ok {
			t.Fatal("could not fill the seat table")
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/events", nil)
	req.Host = "127.0.0.1:8901"
	rec := httptest.NewRecorder()
	s.handleSSE(rec, req)
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("/events past the seat cap = %d (Retry-After %q), want 429 with Retry-After", rec.Code, rec.Header().Get("Retry-After"))
	}
}

func TestInputRefusesABodyPastTheCap(t *testing.T) {
	s := newWiredServer(t)
	body := `{"text":"` + strings.Repeat("a", maxWebSendBody) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/input", strings.NewReader(body))
	req.Host = "127.0.0.1:8901"
	rec := httptest.NewRecorder()
	s.handleInput(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /input with a body past maxWebSendBody = %d, want 400", rec.Code)
	}
}

func TestInputRefusesANullOrigin(t *testing.T) {
	s := newWiredServer(t)
	req := httptest.NewRequest(http.MethodPost, "/input", strings.NewReader(`{"text":"hi"}`))
	req.Host = "127.0.0.1:8901"
	req.Header.Set("Origin", "null")
	rec := httptest.NewRecorder()
	s.handleInput(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST /input with Origin: null = %d, want 403", rec.Code)
	}
}
