package appgen

import (
	"net/http"
	"os/exec"
	"strings"
	"testing"
)

func TestOpenAPICLIMixedCaseJSONResponse(t *testing.T) {
	dir := oaTempModule(t)
	bin := buildOAPets(t, dir, `{"openapi":"3.0.3","paths":{"/pets":{"get":{"operationId":"listPets"}}}}`)
	for _, ct := range []string{"Application/JSON; charset=utf-8", "", "text/plain"} {
		t.Run(ct, func(t *testing.T) {
			srv := newOARecorder(t, "", func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", ct)
				_, _ = w.Write([]byte(`{"id":7}`))
			})
			cmd := exec.Command(bin, "list-pets", "--url", srv.URL)
			cmd.Env = oaCLIEnv(t)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("%v\n%s", err, out)
			}
			pretty := strings.Contains(string(out), `"id": 7`)
			if want := ct == "Application/JSON; charset=utf-8"; pretty != want {
				t.Fatalf("pretty = %v, want %v: %s", pretty, want, out)
			}
		})
	}
}
