package cssweight

import (
	"net/http"
	"strings"
	"testing"
)

func TestSweepGzipSumsEachSheetCompressed(t *testing.T) {
	css := strings.Repeat("a{color:red}", 1000)
	base := serve(t, map[string]string{"/": `<style>` + css + `</style><link rel="stylesheet" href="/a.css">`, "/a.css": css})
	w, err := Measure(http.DefaultClient, base+"/")
	if err != nil {
		t.Fatal(err)
	}
	if want := 2 * gzipLen([]byte(css)); w.Gzip() != want || w.Gzip() >= w.Bytes()/10 {
		t.Errorf("Gzip = %d, want %d, the two sheets compressed on their own", w.Gzip(), want)
	}
}

func TestSweepFirstBaseHrefWins(t *testing.T) {
	base := serve(t, map[string]string{
		"/":          `<base href="/one/"><base href="/two/"><link rel="stylesheet" href="s.css">`,
		"/one/s.css": "x",
		"/two/s.css": "yy",
	})
	w, err := Measure(http.DefaultClient, base+"/")
	if err != nil {
		t.Fatal(err)
	}
	if w.Bytes() != 1 {
		t.Errorf("Bytes = %d, want the 1-byte sheet under the first <base href>", w.Bytes())
	}
}

func TestSweepOversizedSheetIsAnError(t *testing.T) {
	base := serve(t, map[string]string{"/": `<link rel="stylesheet" href="/big.css">`, "/big.css": strings.Repeat("x", maxBody+1)})
	if _, err := Measure(http.DefaultClient, base+"/"); err == nil || !strings.Contains(err.Error(), "over") {
		t.Errorf("Measure of a sheet over maxBody = %v, want an error", err)
	}
}
