package runtime

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestWidgetBadStateStillWires(t *testing.T) {
	var requests atomic.Int32
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Write([]byte(`{}`))
	}))
	defer foreign.Close()
	testHydratedWidgetReopen(t, foreign.URL)
	if requests.Load() != 0 {
		t.Fatal("widget fetched cross-origin state")
	}
}
