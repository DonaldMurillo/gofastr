package semanticcmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

func TestSemanticFailuresAreJSON(t *testing.T) {
	for _, args := range [][]string{{"query", "x", "-k", "abc"}, {"query", "x", "--mmr", "invalid"}, {"query"}, {"query", "x"}, {"stats"}} {
		t.Run("local", func(t *testing.T) {
			covT_unwritableHome(t)
			assertSemanticJSONError(t, args)
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
				assertSemanticJSONError(t, args)
			}
		})
	}
}

func assertSemanticJSONError(t *testing.T, args []string) {
	t.Helper()
	code := 0
	output := clitest.CovT_capStdout(t, func() {
		code = clitest.CovT_capExit(t, func() { RunSemantic(args) })
	})
	var document struct{ Error string }
	if err := json.Unmarshal([]byte(output), &document); err != nil || document.Error == "" || code != 1 {
		t.Errorf("args=%v exit=%d error=%v stdout=%s", args, code, err, output)
	}
}
