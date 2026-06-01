package framework

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestApp wraps an App for in-memory testing (no real HTTP listener).
// Uses httptest.NewRequest + http.Handler.ServeHTTP for speed.
type TestApp struct {
	App    *App
	router http.Handler
}

// TestHarness creates an in-memory test harness around an App.
// No real HTTP server is started — requests go directly through the router.
//
// Calls app.InitPlugins() internally so plugin / battery wiring is in
// place before the first request. Without this, RegisterPlugin'd
// behaviour silently does nothing under the harness (Init never fires).
// Idempotent guard inside InitPlugins makes this safe even if Start is
// called later by the same test.
func TestHarness(t testing.TB, app *App) *TestApp {
	t.Helper()
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("TestHarness: InitPlugins: %v", err)
	}
	return &TestApp{
		App:    app,
		router: app.router,
	}
}

// Get performs a GET request and returns a TestResponse for assertions.
func (ta *TestApp) Get(path string) *TestResponse {
	return ta.doRequest(http.MethodGet, path, nil, nil)
}

// Post performs a POST request with a JSON body.
func (ta *TestApp) Post(path string, body any) *TestResponse {
	return ta.doRequest(http.MethodPost, path, body, nil)
}

// Put performs a PUT request with a JSON body.
func (ta *TestApp) Put(path string, body any) *TestResponse {
	return ta.doRequest(http.MethodPut, path, body, nil)
}

// Delete performs a DELETE request.
func (ta *TestApp) Delete(path string) *TestResponse {
	return ta.doRequest(http.MethodDelete, path, nil, nil)
}

// Request creates a raw TestRequest for further customisation before Execute.
func (ta *TestApp) Request(method, path string, body io.Reader) *TestRequest {
	req := httptest.NewRequest(method, path, body)
	return &TestRequest{
		testApp: ta,
		request: req,
	}
}

// doRequest builds and executes a request in one step.
func (ta *TestApp) doRequest(method, path string, body any, headers map[string]string) *TestResponse {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return &TestResponse{err: err}
		}
		bodyReader = bytes.NewReader(data)
	}

	req := httptest.NewRequest(method, path, bodyReader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	rec := httptest.NewRecorder()
	ta.router.ServeHTTP(rec, req)

	return &TestResponse{recorder: rec}
}

// Close is a no-op for in-memory testing (provided for API consistency).
func (ta *TestApp) Close() {
	_ = ta // no live resources to release under the in-memory harness
}

// ---------------------------------------------------------------------------
// TestRequest — builder for custom requests
// ---------------------------------------------------------------------------

// TestRequest wraps an http.Request with a fluent builder API.
type TestRequest struct {
	testApp *TestApp
	request *http.Request
}

// WithHeader adds a header to the request.
func (tr *TestRequest) WithHeader(key, value string) *TestRequest {
	tr.request.Header.Set(key, value)
	return tr
}

// WithBody sets the request body. Non-reader values are marshalled as JSON.
func (tr *TestRequest) WithBody(body any) *TestRequest {
	var r io.Reader
	if br, ok := body.(io.Reader); ok {
		r = br
	} else {
		data, err := json.Marshal(body)
		if err != nil {
			return tr
		}
		r = bytes.NewReader(data)
	}
	tr.request.Body = io.NopCloser(r)
	if tr.request.Header.Get("Content-Type") == "" {
		tr.request.Header.Set("Content-Type", "application/json")
	}
	return tr
}

// Execute sends the request and returns a TestResponse.
func (tr *TestRequest) Execute() *TestResponse {
	rec := httptest.NewRecorder()
	tr.testApp.router.ServeHTTP(rec, tr.request)
	return &TestResponse{recorder: rec}
}

// ---------------------------------------------------------------------------
// TestResponse — assertions
// ---------------------------------------------------------------------------

// TestResponse wraps the recorded response with assertion helpers.
type TestResponse struct {
	recorder *httptest.ResponseRecorder
	err      error // stored creation error, if any
}

// AssertStatus asserts the HTTP status code. Chainable.
func (tr *TestResponse) AssertStatus(t testing.TB, expected int) *TestResponse {
	t.Helper()
	if tr.err != nil {
		t.Fatalf("request creation error: %v", tr.err)
	}
	if tr.recorder.Code != expected {
		t.Fatalf("expected status %d, got %d; body: %s", expected, tr.recorder.Code, tr.recorder.Body.String())
	}
	return tr
}

// AssertJSON asserts the response body equals expected after JSON normalisation.
// Compares decoded values (not raw strings) so key order and number types don't matter.
func (tr *TestResponse) AssertJSON(t testing.TB, expected any) *TestResponse {
	t.Helper()
	if tr.err != nil {
		t.Fatalf("request creation error: %v", tr.err)
	}

	expectedBytes, err := json.Marshal(expected)
	if err != nil {
		t.Fatalf("marshal expected: %v", err)
	}

	var expNorm, actNorm any
	if err := json.Unmarshal(expectedBytes, &expNorm); err != nil {
		t.Fatalf("unmarshal expected: %v", err)
	}
	if err := json.Unmarshal(tr.recorder.Body.Bytes(), &actNorm); err != nil {
		t.Fatalf("unmarshal response body: %v\nbody: %s", err, tr.recorder.Body.String())
	}

	expJSON, _ := json.Marshal(expNorm)
	actJSON, _ := json.Marshal(actNorm)
	if string(expJSON) != string(actJSON) {
		t.Fatalf("JSON mismatch:\nexpected: %s\nactual:   %s", expJSON, actJSON)
	}
	return tr
}

// AssertHeader asserts a response header value. Chainable.
func (tr *TestResponse) AssertHeader(t testing.TB, key, expected string) *TestResponse {
	t.Helper()
	actual := tr.recorder.Header().Get(key)
	if actual != expected {
		t.Fatalf("expected header %s=%q, got %q", key, expected, actual)
	}
	return tr
}

// AssertBodyContains asserts the body contains the given substring.
func (tr *TestResponse) AssertBodyContains(t testing.TB, substr string) *TestResponse {
	t.Helper()
	if !bytes.Contains(tr.recorder.Body.Bytes(), []byte(substr)) {
		t.Fatalf("expected body to contain %q, got: %s", substr, tr.recorder.Body.String())
	}
	return tr
}

// Body returns the response body as a string.
func (tr *TestResponse) Body() string {
	if tr.recorder != nil {
		return tr.recorder.Body.String()
	}
	return ""
}

// JSON decodes the response body into v.
func (tr *TestResponse) JSON(v any) error {
	return json.Unmarshal(tr.recorder.Body.Bytes(), v)
}

// Status returns the HTTP status code.
func (tr *TestResponse) Status() int {
	if tr.recorder != nil {
		return tr.recorder.Code
	}
	return 0
}

// Close is a no-op (API consistency).
func (tr *TestResponse) Close() {
	_ = tr // no live resources to release; the recorder is in-memory
}
