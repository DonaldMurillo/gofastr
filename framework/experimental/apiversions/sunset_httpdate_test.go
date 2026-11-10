package apiversions

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// The Sunset header (RFC 8594) is an HTTP-date: IMF-fixdate, always GMT
// ("Sun, 06 Nov 1994 08:49:37 GMT"). A client reads it with an HTTP date
// parser. DeprecationMiddleware and DeprecationHeaders formatted it with
// time.RFC1123, which prints the time's own zone abbreviation: "UTC" for
// a UTC time, "EST" for a New York one (with the wall clock unconverted).
func TestSunsetIsAnHTTPDate(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip(err)
	}
	for name, sunset := range map[string]time.Time{
		"utc":   time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC),
		"local": time.Date(2027, 3, 1, 0, 0, 0, 0, ny),
	} {
		t.Run(name+"/middleware", func(t *testing.T) {
			v := Version(router.New(), "v1", WithDeprecation(sunset, "/v2"))
			rec := httptest.NewRecorder()
			v.DeprecationMiddleware()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).
				ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/x", nil))
			checkSunset(t, rec.Header().Get("Sunset"), sunset)
		})
		t.Run(name+"/helper", func(t *testing.T) {
			rec := httptest.NewRecorder()
			DeprecationHeaders(rec, sunset, "/v2")
			checkSunset(t, rec.Header().Get("Sunset"), sunset)
		})
	}
}

func checkSunset(t *testing.T, got string, want time.Time) {
	t.Helper()
	parsed, err := http.ParseTime(got)
	if err != nil {
		t.Fatalf("Sunset %q is not an HTTP-date: %v (want %q)", got, err, want.UTC().Format(http.TimeFormat))
	}
	if !parsed.Equal(want) {
		t.Fatalf("Sunset %q parses to %s, want the instant %s", got, parsed.UTC(), want.UTC())
	}
}
