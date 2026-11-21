package semanticcmd

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/semantic"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

func TestSemanticEncodeFailureExits(t *testing.T) {
	output := clitest.CovT_capStdout(t, func() {
		defer cli.BeginJSON()()
		code := clitest.CovT_capExit(t, func() {
			printHits([]semantic.Hit{{Score: math.NaN()}})
		})
		if code != 1 {
			t.Fatalf("encode failure exit = %d, want 1", code)
		}
	})
	var document struct{ Error string }
	if err := json.Unmarshal([]byte(output), &document); err != nil || document.Error == "" {
		t.Fatalf("encode failure = %q, error = %v", output, err)
	}
}

func TestSemanticFailuresAreJSON(t *testing.T) {
	// A mistake in the command line is bad usage, exit 2; a failing
	// index or remote is an operational error, exit 1.
	for _, tc := range []struct {
		args     []string
		wantCode int
	}{
		{[]string{"query", "x", "-k", "abc"}, 2},
		{[]string{"query", "x", "--mmr", "invalid"}, 2},
		{[]string{"query"}, 2},
		{[]string{"query", "x"}, 1},
		{[]string{"stats"}, 1},
	} {
		t.Run("local", func(t *testing.T) {
			covT_unwritableHome(t)
			assertSemanticJSONError(t, tc.args, tc.wantCode)
		})
	}
	for _, status := range []int{http.StatusBadGateway, http.StatusOK} {
		t.Run("remote", func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				w.Write([]byte("not a JSON response"))
			}))
			defer srv.Close()
			t.Setenv("GOFASTR_URL", srv.URL)
			for _, args := range [][]string{{"query", "x"}, {"stats"}} {
				assertSemanticJSONError(t, args, 1)
			}
		})
	}
}

func assertSemanticJSONError(t *testing.T, args []string, wantCode int) {
	t.Helper()
	code := 0
	output := clitest.CovT_capStdout(t, func() {
		code = clitest.CovT_capExit(t, func() { RunSemantic(args) })
	})
	var document struct{ Error string }
	if err := json.Unmarshal([]byte(output), &document); err != nil || document.Error == "" || code != wantCode {
		t.Errorf("args=%v exit=%d want=%d error=%v stdout=%s", args, code, wantCode, err, output)
	}
}
