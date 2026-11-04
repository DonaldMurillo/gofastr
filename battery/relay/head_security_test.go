package relay

import (
	"net/http"
	"testing"
)

func TestHeadIgnoresRepresentationType(t *testing.T) {
	base := startRelay(t, func(upstream string) Config {
		return Config{Routes: []Route{{Prefix: "e/", Upstream: upstream, Methods: []string{http.MethodGet, http.MethodHead}}}}
	}, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Content-Length", "12")
		if r.Method != http.MethodHead {
			_, _ = w.Write([]byte("<p>hello</p>"))
		}
	})
	for _, method := range []string{http.MethodHead, http.MethodGet} {
		request, err := http.NewRequest(method, base+"/__gofastr/t/e/x", nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		want := http.StatusBadGateway
		if method == http.MethodHead {
			want = http.StatusOK
		}
		if response.StatusCode != want {
			t.Errorf("%s: status=%d, want %d", method, response.StatusCode, want)
		}
	}
}
