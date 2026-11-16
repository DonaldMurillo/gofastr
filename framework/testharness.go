package framework

import (
	"bytes"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
	"github.com/DonaldMurillo/gofastr/internal/jsonexact"
	"github.com/DonaldMurillo/gofastr/internal/renderdiag"
)

// TestApp drives an App's router in memory, with no listener.
type TestApp struct {
	App    *App
	router http.Handler
	user   any
	tenant any
}

// AsUser returns a copy of the harness whose requests carry user as the
// authenticated caller (core/handler.SetUser). Any non-nil value passes
// auto-CRUD's default session gate.
func (ta *TestApp) AsUser(user any) *TestApp {
	return &TestApp{App: ta.App, router: ta.router, user: user, tenant: ta.tenant}
}

// AsTenant returns a copy of the harness whose requests carry a tenant in context.
func (ta *TestApp) AsTenant(t any) *TestApp {
	return &TestApp{App: ta.App, router: ta.router, user: ta.user, tenant: t}
}

// TestHarness returns an in-memory harness around app. It runs
// InitPlugins, records semantic coverage, and fails the test on a render
// diagnostic or retired markup in a response.
func TestHarness(t testing.TB, app *App) *TestApp {
	t.Helper()
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("TestHarness: InitPlugins: %v", err)
	}
	RecordSemanticCoverage(t, app)
	return &TestApp{
		App: app,
		router: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := renderdiag.WithObserver(r.Context(), func(message string) {
				t.Helper()
				t.Errorf("%s %s: %s", r.Method, r.URL.Path, message)
			})
			ctx = renderdiag.WithRetiredReporter(ctx, func(message string) {
				t.Helper()
				t.Errorf("%s %s: %s", r.Method, r.URL.Path, message)
			})
			app.router.ServeHTTP(w, r.WithContext(ctx))
		}),
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
	if ta.user != nil {
		req = req.WithContext(handler.SetUser(req.Context(), ta.user))
	}
	if ta.tenant != nil {
		req = req.WithContext(handler.SetTenant(req.Context(), ta.tenant))
		if tenantStr, ok := ta.tenant.(string); ok {
			req = req.WithContext(tenant.SetTenantID(req.Context(), tenantStr))
		}
	}
	return &TestRequest{
		testApp: ta,
		request: req,
	}
}

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
	if ta.user != nil {
		req = req.WithContext(handler.SetUser(req.Context(), ta.user))
	}
	if ta.tenant != nil {
		req = req.WithContext(handler.SetTenant(req.Context(), ta.tenant))
		if tenantStr, ok := ta.tenant.(string); ok {
			req = req.WithContext(tenant.SetTenantID(req.Context(), tenantStr))
		}
	}

	rec := httptest.NewRecorder()
	ta.router.ServeHTTP(rec, req)

	return &TestResponse{recorder: rec}
}

// Close is a no-op.
func (ta *TestApp) Close() {
	_ = ta
}

// TestRequest wraps an http.Request with a fluent builder API.
type TestRequest struct {
	testApp *TestApp
	request *http.Request
	err     error
}

// WithHeader adds a header to the request.
func (tr *TestRequest) WithHeader(key, value string) *TestRequest {
	tr.request.Header.Set(key, value)
	return tr
}

// WithBody sets the request body, marshalling a non-reader as JSON. A
// marshal failure fails the response Execute returns.
func (tr *TestRequest) WithBody(body any) *TestRequest {
	var r io.Reader
	if br, ok := body.(io.Reader); ok {
		r = br
	} else {
		data, err := json.Marshal(body)
		if err != nil {
			tr.err = err
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

// Execute sends the request, or returns a failed TestResponse when the
// builder recorded an error.
func (tr *TestRequest) Execute() *TestResponse {
	if tr.err != nil {
		return &TestResponse{err: tr.err}
	}
	rec := httptest.NewRecorder()
	tr.testApp.router.ServeHTTP(rec, tr.request)
	return &TestResponse{recorder: rec}
}

// TestResponse wraps the recorded response with assertion helpers.
type TestResponse struct {
	recorder *httptest.ResponseRecorder
	err      error
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

// AssertJSON asserts the response body decodes to the same JSON value as
// expected, ignoring key order and number spelling. Chainable.
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
	if err := jsonexact.Decode(expectedBytes, &expNorm, canonicalNumber); err != nil {
		t.Fatalf("unmarshal expected: %v", err)
	}
	if err := jsonexact.Decode(tr.recorder.Body.Bytes(), &actNorm, canonicalNumber); err != nil {
		t.Fatalf("unmarshal response body: %v\nbody: %s", err, tr.recorder.Body.String())
	}

	expJSON, expErr := json.Marshal(expNorm)
	actJSON, actErr := json.Marshal(actNorm)
	if expErr != nil || actErr != nil {
		t.Fatalf("marshal normalized JSON: exp %v, act %v", expErr, actErr)
	}
	if string(expJSON) != string(actJSON) {
		t.Fatalf("JSON mismatch:\nexpected: %s\nactual:   %s", expJSON, actJSON)
	}
	return tr
}

func canonicalNumber(n json.Number) (any, error) {
	v, err := jsonexact.Lossless(n)
	if err != nil {
		return nil, err
	}
	if _, ok := v.(json.Number); ok {
		if r, ok := new(big.Rat).SetString(n.String()); ok && r.IsInt() {
			return json.Number(r.Num().String()), nil
		}
	}
	return v, nil
}

// AssertHeader asserts a response header value. Chainable.
func (tr *TestResponse) AssertHeader(t testing.TB, key, expected string) *TestResponse {
	t.Helper()
	if tr.err != nil {
		t.Fatalf("request creation error: %v", tr.err)
	}
	actual := tr.recorder.Header().Get(key)
	if actual != expected {
		t.Fatalf("expected header %s=%q, got %q", key, expected, actual)
	}
	return tr
}

// AssertBodyContains asserts the body contains the given substring.
func (tr *TestResponse) AssertBodyContains(t testing.TB, substr string) *TestResponse {
	t.Helper()
	if tr.err != nil {
		t.Fatalf("request creation error: %v", tr.err)
	}
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
	if tr.err != nil {
		return tr.err
	}
	return json.Unmarshal(tr.recorder.Body.Bytes(), v)
}

// Status returns the HTTP status code.
func (tr *TestResponse) Status() int {
	if tr.recorder != nil {
		return tr.recorder.Code
	}
	return 0
}

// Close is a no-op.
func (tr *TestResponse) Close() {
	_ = tr
}
