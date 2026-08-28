package middleware

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func stampedRequest(budget time.Duration) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/reports/big", nil)
	return req.WithContext(WithRouteTimeout(req.Context(), RouteTimeout{
		Method:  http.MethodGet,
		Pattern: "/reports/{id}",
		Budget:  budget,
	}))
}

func TestTimeoutRouteOverrideLengthens(t *testing.T) {
	handler := Timeout(30 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(120 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, stampedRequest(2*time.Second))
	if rec.Code != http.StatusOK {
		t.Errorf("route budget above sleep must serve 200, got %d", rec.Code)
	}
}

func TestTimeoutRouteOverrideShortens(t *testing.T) {
	handler := Timeout(2 * time.Second)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	start := time.Now()
	handler.ServeHTTP(rec, stampedRequest(40*time.Millisecond))
	if rec.Code != http.StatusGatewayTimeout {
		t.Errorf("route budget below sleep must 504, got %d", rec.Code)
	}
	if time.Since(start) > time.Second {
		t.Errorf("504 should arrive at the route budget, not the default")
	}
}

func TestTimeoutNoTimeoutBudgetExempts(t *testing.T) {
	handler := Timeout(30 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(120 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, stampedRequest(-1)) // router.NoTimeout stamps a negative budget
	if rec.Code != http.StatusOK {
		t.Errorf("negative budget must exempt the route, got %d", rec.Code)
	}
}

func TestTimeoutFireLogsMethodAndPattern(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(prev)

	handler := Timeout(20 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, stampedRequest(20*time.Millisecond))
	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("expected 504, got %d", rec.Code)
	}
	log := buf.String()
	for _, want := range []string{"request timeout", "method=GET", "pattern=/reports/{id}", "path=/reports/big"} {
		if !strings.Contains(log, want) {
			t.Errorf("timeout log missing %q:\n%s", want, log)
		}
	}
}

func TestTimeoutStreamingPastDeadlineDoesNotLog(t *testing.T) {
	// A flushed response sheds the deadline; the timer still fires but
	// no 504 is written, so no "request timeout" line may appear.
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(prev)

	handler := Timeout(20 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.(http.Flusher).Flush()
		time.Sleep(80 * time.Millisecond)
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, stampedRequest(20*time.Millisecond))
	if strings.Contains(buf.String(), "request timeout") {
		t.Errorf("streaming response must not log a timeout:\n%s", buf.String())
	}
}

func TestTimeoutClientDisconnectDoesNotLog(t *testing.T) {
	// A client that leaves mid-handler abandons the request through the
	// same path as the deadline, but it is not a timeout and must not
	// log as one.
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(prev)

	handler := Timeout(5 * time.Second)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	ctx, cancel := context.WithCancel(context.Background())
	req := stampedRequest(5 * time.Second).WithContext(WithRouteTimeout(ctx, RouteTimeout{
		Method: "GET", Pattern: "/reports/{id}", Budget: 5 * time.Second,
	}))
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if strings.Contains(buf.String(), "request timeout") {
		t.Errorf("client disconnect must not log a timeout:\n%s", buf.String())
	}
}

func TestTimeoutZeroBudgetExempts(t *testing.T) {
	handler := Timeout(30 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(120 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, stampedRequest(0))
	if rec.Code != http.StatusOK {
		t.Errorf("zero budget means no timeout (net/http convention), got %d", rec.Code)
	}
}

func TestRouteTimeoutFromContextRoundTrip(t *testing.T) {
	rt := RouteTimeout{Method: "POST", Pattern: "/x", Budget: time.Minute}
	got, ok := RouteTimeoutFromContext(WithRouteTimeout(context.Background(), rt))
	if !ok || got != rt {
		t.Errorf("round trip failed: got %+v ok=%v", got, ok)
	}
	if _, ok := RouteTimeoutFromContext(context.Background()); ok {
		t.Error("bare context must report no route timeout")
	}
}
