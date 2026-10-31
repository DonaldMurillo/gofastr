package a

import "net/http"

// Test doubles sit in no production chain: both arms are quiet here.
type testRecorder struct{ http.ResponseWriter }

func (t *testRecorder) WriteHeader(int) {}

func useTestRecorder(next http.Handler, w http.ResponseWriter, r *http.Request) {
	next.ServeHTTP(&testRecorder{w}, r)
	next.ServeHTTP(w, r)
	w.Header().Set("X-Test", "1")
}

func useOnlyTested(next http.Handler, w http.ResponseWriter, r *http.Request) {
	next.ServeHTTP(&onlyTested{w}, r)
}
