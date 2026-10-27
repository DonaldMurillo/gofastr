package framework

import (
	"net/http"
	"testing"
)

// AssertJSON normalised both sides through float64, so a body holding
// 9007199254740992 matched an expected 9007199254740993.
func TestAssertJSONComparesBigIntegers(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	app.Router().Get("/n", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":9007199254740992,"big":1e20}`))
	}))
	app.Router().Get("/odd", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":9007199254740993}`))
	}))
	ta := TestHarness(t, app)

	if !cov_runFail(func(tb testing.TB) {
		ta.Get("/odd").AssertJSON(tb, map[string]any{"id": int64(9007199254740992)})
	}) {
		t.Error("AssertJSON matched a body of 9007199254740993 against 9007199254740992")
	}
	ta.Get("/odd").AssertJSON(t, map[string]any{"id": int64(9007199254740993)})

	if !cov_runFail(func(tb testing.TB) {
		ta.Get("/n").AssertJSON(tb, map[string]any{"id": int64(9007199254740993), "big": 1e20})
	}) {
		t.Error("AssertJSON matched 9007199254740993 against 9007199254740992")
	}
	// The same number spelled two ways still matches.
	ta.Get("/n").AssertJSON(t, map[string]any{"id": int64(9007199254740992), "big": 1e20})
}
