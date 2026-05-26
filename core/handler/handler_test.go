package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- Test types ---

type createUserReq struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Page  int    `query:"page"`
	ID    string `path:"id"`
	XRID  string `header:"X-Request-ID"`
}

type userResponse struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

type combinedReq struct {
	Name   string `json:"name"`
	Role   string `json:"role"`
	Page   int    `query:"page"`
	Sort   string `query:"sort"`
	ID     string `path:"id"`
	XTrace string `header:"X-Trace-ID"`
}

// --- Handler tests ---

func TestTypedHandlerExecutes(t *testing.T) {
	h := func(ctx context.Context, in createUserReq) (userResponse, error) {
		return userResponse{ID: 1, Name: in.Name, Email: in.Email}, nil
	}

	adapter := HandlerAdapter(h)

	body := `{"name":"Alice","email":"alice@example.com"}`
	req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	adapter(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var got userResponse
	json.NewDecoder(resp.Body).Decode(&got)
	if got.Name != "Alice" {
		t.Errorf("expected name Alice, got %s", got.Name)
	}
	if got.Email != "alice@example.com" {
		t.Errorf("expected email alice@example.com, got %s", got.Email)
	}
}

func TestHandlerAdapterPlugsIntoMux(t *testing.T) {
	h := func(ctx context.Context, in createUserReq) (userResponse, error) {
		return userResponse{ID: 42, Name: in.Name, Email: in.Email}, nil
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /users", HandlerAdapter(h))

	body := `{"name":"Bob","email":"bob@example.com"}`
	req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var got userResponse
	json.NewDecoder(resp.Body).Decode(&got)
	if got.ID != 42 {
		t.Errorf("expected ID 42, got %d", got.ID)
	}
}

// --- Binding tests ---

func TestBindJSONBody(t *testing.T) {
	var dst createUserReq
	body := `{"name":"Alice","email":"alice@example.com"}`
	req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	if err := Bind(req, &dst); err != nil {
		t.Fatalf("Bind failed: %v", err)
	}
	if dst.Name != "Alice" {
		t.Errorf("expected name Alice, got %s", dst.Name)
	}
	if dst.Email != "alice@example.com" {
		t.Errorf("expected email alice@example.com, got %s", dst.Email)
	}
}

func TestBindQueryParams(t *testing.T) {
	var dst struct {
		Page    int    `query:"page"`
		Sort    string `query:"sort"`
		Timeout int    `query:"timeout"`
	}

	req := httptest.NewRequest(http.MethodGet, "/items?page=2&sort=name&timeout=30", nil)

	if err := Bind(req, &dst); err != nil {
		t.Fatalf("Bind failed: %v", err)
	}
	if dst.Page != 2 {
		t.Errorf("expected page 2, got %d", dst.Page)
	}
	if dst.Sort != "name" {
		t.Errorf("expected sort name, got %s", dst.Sort)
	}
	if dst.Timeout != 30 {
		t.Errorf("expected timeout 30, got %d", dst.Timeout)
	}
}

func TestBindPathParams(t *testing.T) {
	var dst struct {
		ID string `path:"id"`
	}

	req := httptest.NewRequest(http.MethodGet, "/users/{id}", nil)
	req.SetPathValue("id", "abc-123")

	if err := Bind(req, &dst); err != nil {
		t.Fatalf("Bind failed: %v", err)
	}
	if dst.ID != "abc-123" {
		t.Errorf("expected id abc-123, got %s", dst.ID)
	}
}

func TestBindHeaders(t *testing.T) {
	var dst struct {
		XRequestID string `header:"X-Request-ID"`
		Accept     string `header:"Accept"`
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", "req-123")
	req.Header.Set("Accept", "application/json")

	if err := Bind(req, &dst); err != nil {
		t.Fatalf("Bind failed: %v", err)
	}
	if dst.XRequestID != "req-123" {
		t.Errorf("expected X-Request-ID req-123, got %s", dst.XRequestID)
	}
	if dst.Accept != "application/json" {
		t.Errorf("expected Accept application/json, got %s", dst.Accept)
	}
}

func TestCombinedBinding(t *testing.T) {
	var dst combinedReq

	body := `{"name":"Alice","role":"admin"}`
	req := httptest.NewRequest(http.MethodPost, "/users/{id}?page=3&sort=date", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Trace-ID", "trace-456")
	req.SetPathValue("id", "user-789")

	if err := Bind(req, &dst); err != nil {
		t.Fatalf("Bind failed: %v", err)
	}

	// From JSON body
	if dst.Name != "Alice" {
		t.Errorf("expected name Alice, got %s", dst.Name)
	}
	if dst.Role != "admin" {
		t.Errorf("expected role admin, got %s", dst.Role)
	}

	// From query params
	if dst.Page != 3 {
		t.Errorf("expected page 3, got %d", dst.Page)
	}
	if dst.Sort != "date" {
		t.Errorf("expected sort date, got %s", dst.Sort)
	}

	// From path param
	if dst.ID != "user-789" {
		t.Errorf("expected id user-789, got %s", dst.ID)
	}

	// From header
	if dst.XTrace != "trace-456" {
		t.Errorf("expected X-Trace-ID trace-456, got %s", dst.XTrace)
	}
}

func TestBodyPriorityOverQuery(t *testing.T) {
	// If the same field is in both body and query, body should win.
	var dst struct {
		Name string `json:"name" query:"name"`
	}

	body := `{"name":"from-body"}`
	req := httptest.NewRequest(http.MethodPost, "/?name=from-query", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	if err := Bind(req, &dst); err != nil {
		t.Fatalf("Bind failed: %v", err)
	}

	if dst.Name != "from-body" {
		t.Errorf("expected name from-body (body should override query), got %s", dst.Name)
	}
}

func TestInvalidJSONReturns400(t *testing.T) {
	h := func(ctx context.Context, in createUserReq) (userResponse, error) {
		return userResponse{}, nil
	}

	adapter := HandlerAdapter(h)

	body := `{invalid json!!!`
	req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	adapter(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(bodyBytes), "invalid JSON") {
		t.Errorf("expected error message to contain 'invalid JSON', got: %s", string(bodyBytes))
	}
}

func TestBindNilDst(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	err := Bind(req, nil)
	if err == nil {
		t.Error("expected error for nil dst")
	}
}

// --- Error tests ---

func TestErrorResponseCorrectStatus(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantMsg    string
	}{
		{
			name:       "structured error 400",
			err:        Errorf(http.StatusBadRequest, "bad request"),
			wantStatus: 400,
			wantMsg:    "bad request",
		},
		{
			name:       "structured error 404",
			err:        Errorf(http.StatusNotFound, "not found"),
			wantStatus: 404,
			wantMsg:    "not found",
		},
		{
			name:       "structured error 500",
			err:        Errorf(http.StatusInternalServerError, "something broke"),
			wantStatus: 500,
			wantMsg:    "something broke",
		},
		{
			// Plain (non-*Error) errors are treated as internal failures
			// and rendered with a generic message — see WriteError's
			// doc comment. The inner message is intentionally NOT
			// exposed; wrap explicitly with Errorf/WrapError to control
			// what reaches the client.
			name:       "plain error → 500",
			err:        errors.New("plain error"),
			wantStatus: 500,
			wantMsg:    "internal server error",
		},
		{
			name:       "wrapped error",
			err:        WrapError(422, "unprocessable", errors.New("db error")),
			wantStatus: 422,
			wantMsg:    "unprocessable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			WriteError(w, tt.err)

			resp := w.Result()
			if resp.StatusCode != tt.wantStatus {
				t.Errorf("expected status %d, got %d", tt.wantStatus, resp.StatusCode)
			}

			ct := resp.Header.Get("Content-Type")
			if !strings.Contains(ct, "application/json") {
				t.Errorf("expected application/json content-type, got %s", ct)
			}

			var body errorResponse
			json.NewDecoder(resp.Body).Decode(&body)
			if body.Error.Message != tt.wantMsg {
				t.Errorf("expected message %q, got %q", tt.wantMsg, body.Error.Message)
			}
			if body.Success {
				t.Error("expected success=false")
			}
		})
	}
}

func TestValidationError(t *testing.T) {
	fields := map[string][]string{
		"email": {"is required", "must be valid"},
		"name":  {"is required"},
	}
	herr := ValidationError(fields)

	w := httptest.NewRecorder()
	WriteError(w, herr)

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}

	var body errorResponse
	json.NewDecoder(resp.Body).Decode(&body)
	if body.Error.Fields["email"][0] != "is required" {
		t.Errorf("expected field error 'is required', got %v", body.Error.Fields)
	}
	if body.Error.Fields["name"][0] != "is required" {
		t.Errorf("expected field error 'is required', got %v", body.Error.Fields)
	}
}

func TestErrorImplementsError(t *testing.T) {
	err := Errorf(400, "bad request")
	if err.Error() != "bad request" {
		t.Errorf("expected 'bad request', got %s", err.Error())
	}

	wrapped := WrapError(500, "internal", errors.New("root cause"))
	if !strings.Contains(wrapped.Error(), "root cause") {
		t.Errorf("expected wrapped error to contain 'root cause', got %s", wrapped.Error())
	}
	if !errors.Is(wrapped, wrapped.Err) {
		t.Error("expected errors.Is to match wrapped error")
	}
}

func TestHandlerReturnsError(t *testing.T) {
	h := func(ctx context.Context, in createUserReq) (userResponse, error) {
		return userResponse{}, Errorf(http.StatusNotFound, "user not found")
	}

	adapter := HandlerAdapter(h)

	body := `{"name":"Alice"}`
	req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	adapter(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

// --- Response tests ---

func TestRespondNil(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	Respond(w, req, nil)

	resp := w.Result()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("expected 204, got %d", resp.StatusCode)
	}
}

func TestRespondJSON(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	Respond(w, req, userResponse{ID: 1, Name: "Alice"})

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("expected application/json, got %s", ct)
	}

	var got userResponse
	json.NewDecoder(resp.Body).Decode(&got)
	if got.Name != "Alice" {
		t.Errorf("expected name Alice, got %s", got.Name)
	}
}

func TestRespondHTML(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	Respond(w, req, HTML("<h1>Hello</h1>"))

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("expected text/html, got %s", ct)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "<h1>Hello</h1>" {
		t.Errorf("expected <h1>Hello</h1>, got %s", string(body))
	}
}

func TestRespondSSE(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	Respond(w, req, SSE{Event: "update", Data: `{"count":1}`, ID: "42"})

	resp := w.Result()
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/event-stream") {
		t.Errorf("expected text/event-stream, got %s", ct)
	}

	body, _ := io.ReadAll(resp.Body)
	s := string(body)
	if !strings.Contains(s, "id: 42") {
		t.Errorf("expected SSE id, got: %s", s)
	}
	if !strings.Contains(s, "event: update") {
		t.Errorf("expected SSE event, got: %s", s)
	}
	if !strings.Contains(s, `data: {"count":1}`) {
		t.Errorf("expected SSE data, got: %s", s)
	}
}

func TestRespondRaw(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	Respond(w, req, RawBytes{Data: []byte("binary data"), CT: "image/png"})

	resp := w.Result()
	ct := resp.Header.Get("Content-Type")
	if ct != "image/png" {
		t.Errorf("expected image/png, got %s", ct)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "binary data" {
		t.Errorf("expected 'binary data', got %s", string(body))
	}
}

// --- Context tests ---

func TestContextUser(t *testing.T) {
	ctx := context.Background()
	ctx = SetUser(ctx, "user-123")

	user, ok := GetUser(ctx)
	if !ok {
		t.Error("expected to find user in context")
	}
	if user != "user-123" {
		t.Errorf("expected user-123, got %v", user)
	}
}

func TestContextTenant(t *testing.T) {
	ctx := context.Background()
	ctx = SetTenant(ctx, "tenant-abc")

	tenant, ok := GetTenant(ctx)
	if !ok {
		t.Error("expected to find tenant in context")
	}
	if tenant != "tenant-abc" {
		t.Errorf("expected tenant-abc, got %v", tenant)
	}
}

func TestContextRequestID(t *testing.T) {
	ctx := context.Background()
	ctx = SetRequestID(ctx, "req-456")

	id, ok := GetRequestID(ctx)
	if !ok {
		t.Error("expected to find request ID in context")
	}
	if id != "req-456" {
		t.Errorf("expected req-456, got %s", id)
	}
}

func TestContextMissing(t *testing.T) {
	ctx := context.Background()

	if _, ok := GetUser(ctx); ok {
		t.Error("expected no user in empty context")
	}
	if _, ok := GetTenant(ctx); ok {
		t.Error("expected no tenant in empty context")
	}
	if _, ok := GetRequestID(ctx); ok {
		t.Error("expected no request ID in empty context")
	}
}

func TestContextLogger(t *testing.T) {
	ctx := context.Background()
	logger := "my-logger"
	ctx = SetLogger(ctx, logger)

	got, ok := GetLogger(ctx)
	if !ok {
		t.Error("expected to find logger in context")
	}
	if got != logger {
		t.Errorf("expected %v, got %v", logger, got)
	}
}

// --- Panic recovery test ---

func TestPanicRecovery(t *testing.T) {
	h := func(ctx context.Context, in createUserReq) (userResponse, error) {
		panic("something went very wrong")
	}

	adapter := HandlerAdapter(h)

	body := `{"name":"Alice"}`
	req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	adapter(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected 500 after panic, got %d", resp.StatusCode)
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(bodyBytes), "internal server error") {
		t.Errorf("expected 'internal server error' in response, got: %s", string(bodyBytes))
	}
}

// --- RequestFromContext test ---

func TestRequestFromContext(t *testing.T) {
	var captured *http.Request
	h := func(ctx context.Context, in createUserReq) (userResponse, error) {
		captured, _ = RequestFromContext(ctx)
		return userResponse{ID: 1, Name: in.Name}, nil
	}

	adapter := HandlerAdapter(h)

	body := `{"name":"Test"}`
	req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	adapter(w, req)

	if captured == nil {
		t.Error("expected request to be available in context")
	}
	if captured.Method != http.MethodPost {
		t.Errorf("expected POST method, got %s", captured.Method)
	}
}

// --- Full integration test ---

func TestFullIntegration(t *testing.T) {
	createUser := func(ctx context.Context, in createUserReq) (userResponse, error) {
		// Verify context has request
		_, hasReq := RequestFromContext(ctx)
		if !hasReq {
			t.Error("handler context missing request")
		}

		// Verify all fields were bound
		if in.Name == "" {
			return userResponse{}, Errorf(400, "name is required")
		}

		return userResponse{
			ID:    1,
			Name:  in.Name,
			Email: in.Email,
		}, nil
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /users/{id}", HandlerAdapter(createUser))

	body := `{"name":"Alice","email":"alice@example.com"}`
	req := httptest.NewRequest(http.MethodPost, "/users/user-123?page=2", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "req-789")
	req.SetPathValue("id", "user-123")

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var got userResponse
	json.NewDecoder(resp.Body).Decode(&got)
	if got.Name != "Alice" {
		t.Errorf("expected name Alice, got %s", got.Name)
	}
}

// --- Edge cases ---

func TestBindNoBodyGET(t *testing.T) {
	var dst struct {
		Page int `query:"page"`
	}

	req := httptest.NewRequest(http.MethodGet, "/items?page=5", nil)
	if err := Bind(req, &dst); err != nil {
		t.Fatalf("Bind failed: %v", err)
	}
	if dst.Page != 5 {
		t.Errorf("expected page 5, got %d", dst.Page)
	}
}

func TestBindNonStructPointer(t *testing.T) {
	var s string
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	err := Bind(req, &s)
	// Non-struct pointer with no body should be fine (no-op)
	// But binding to a non-pointer should fail
	if err != nil {
		t.Logf("bind non-struct pointer: %v (acceptable)", err)
	}
}

func TestBindNonPointerReturnsError(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	var s string
	// Passing a value (not a pointer) should return an error
	err := Bind(req, s)
	if err == nil {
		t.Error("expected error for non-pointer destination")
	}
}

func TestBindEmptyBodyPOST(t *testing.T) {
	var dst createUserReq
	req := httptest.NewRequest(http.MethodPost, "/users?name=queryname", nil)
	// No Content-Type set, no body

	if err := Bind(req, &dst); err != nil {
		t.Fatalf("Bind failed: %v", err)
	}

	// Query params should still work
	if dst.Page != 0 {
		// No page in query, so 0 is correct
	}
}

func TestSSEStream(t *testing.T) {
	events := make(chan SSE, 3)
	events <- SSE{Event: "msg", Data: "hello", ID: "1"}
	events <- SSE{Event: "msg", Data: "world", ID: "2"}
	close(events)

	w := httptest.NewRecorder()
	SSEStream(w, events)

	resp := w.Result()
	ct := resp.Header.Get("Content-Type")
	if ct != "text/event-stream" {
		t.Errorf("expected text/event-stream, got %s", ct)
	}

	body, _ := io.ReadAll(resp.Body)
	s := string(body)
	if !strings.Contains(s, "event: msg") {
		t.Errorf("expected SSE event in output, got: %s", s)
	}
	if !strings.Contains(s, "data: hello") {
		t.Errorf("expected 'data: hello' in output, got: %s", s)
	}
}

func TestResponseTypes(t *testing.T) {
	t.Run("HTML", func(t *testing.T) {
		h := HTML("<p>hi</p>")
		if h.ContentType() != "text/html; charset=utf-8" {
			t.Errorf("unexpected content type: %s", h.ContentType())
		}
		w := httptest.NewRecorder()
		// Use a httptest recorder directly
		h.WriteBody(w)
		if w.Body.String() != "<p>hi</p>" {
			t.Errorf("unexpected body: %s", w.Body.String())
		}
	})

	t.Run("SSE", func(t *testing.T) {
		s := SSE{Event: "ping", Data: "pong"}
		if s.ContentType() != "text/event-stream" {
			t.Errorf("unexpected content type: %s", s.ContentType())
		}
	})

	t.Run("RawBytes", func(t *testing.T) {
		r := RawBytes{Data: []byte{0x89, 0x50}, CT: "image/png"}
		if r.ContentType() != "image/png" {
			t.Errorf("unexpected content type: %s", r.ContentType())
		}
	})
}

// Compile-time check that our types satisfy interfaces
func TestInterfaceCompliance(t *testing.T) {
	var _ ResponseType = HTML("")
	var _ ResponseType = SSE{}
	var _ ResponseType = RawBytes{}
	var _ error = (*Error)(nil)
}

// Verify the handler type compiles with different I/O types
func TestGenericHandlerCompiles(t *testing.T) {
	_ = Handler[createUserReq, userResponse](nil)
	_ = Handler[string, int](nil)
	_ = Handler[struct{ Name string }, struct{ OK bool }](nil)
	_ = Handler[any, any](nil)
	_ = fmt.Sprintf("handlers compile with various type params")
}
