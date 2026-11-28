package devcmd

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestDevHoldHeadersNameThePhase(t *testing.T) {
	h := newTestHold(t)
	h.show(holdBuildFailed, "boom")
	res, _ := holdGet(t, h.addr, "")
	if got := res.Header.Get("X-Gofastr-Dev"); got != holdBuildFailed {
		t.Errorf("X-Gofastr-Dev = %q, want %q", got, holdBuildFailed)
	}
	if got := res.Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	req, err := http.NewRequest(http.MethodHead, "http://"+h.addr+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	head, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer head.Body.Close()
	if b, _ := io.ReadAll(head.Body); head.StatusCode != http.StatusServiceUnavailable || len(b) != 0 {
		t.Errorf("HEAD: status %d with %d body bytes, want 503 and none", head.StatusCode, len(b))
	}
}

func TestDevHoldServesItsReloadScript(t *testing.T) {
	h := newTestHold(t)
	res, err := (&http.Client{Timeout: 5 * time.Second}).Get("http://" + h.addr + holdScriptURL)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/javascript") || res.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("%s: status %d, %q, %q", holdScriptURL, res.StatusCode, res.Header.Get("Content-Type"), res.Header.Get("Cache-Control"))
	}
	if !strings.Contains(string(b), "if (e.data !== id) location.reload()") {
		t.Errorf("the hold's reload client does not reload on a build ID other than its own:\n%s", b)
	}
	_, page := holdGet(t, h.addr, "text/html")
	if !strings.Contains(page, `<script src="`+holdScriptURL+`" data-hold-id="`) {
		t.Errorf("the hold page does not load its reload client:\n%s", page)
	}
}

func TestDevHoldKeepsTheTailOfLongOutput(t *testing.T) {
	long := strings.Repeat("x", holdDetailLimit) + "the last error"
	if got := tailString(long, holdDetailLimit); len(got) > holdDetailLimit+len("…") || !strings.HasSuffix(got, "the last error") || !strings.HasPrefix(got, "…") {
		t.Errorf("tailString kept %d bytes, ending %q", len(got), got[max(0, len(got)-20):])
	}
	var o holdOutput
	for _, c := range "xyz" {
		_, _ = o.Write([]byte(strings.Repeat(string(c), holdDetailLimit)))
	}
	_, _ = o.Write([]byte("tail"))
	if s := o.String(); len(s) > 2*holdDetailLimit || !strings.HasSuffix(s, "tail") || strings.Contains(s, "x") {
		t.Errorf("holdOutput holds %d bytes, ending %q, the oldest dropped: %v", len(s), s[max(0, len(s)-10):], !strings.Contains(s, "x"))
	}
}

func TestDevHoldStripsEscapesFromTheOutput(t *testing.T) {
	h := newTestHold(t)
	h.show(holdBuildFailed, "\x1b[31m./main.go:3: undefined: x\x1b[0m")
	if _, body := holdGet(t, h.addr, ""); strings.Contains(body, "\x1b") || !strings.Contains(body, "undefined: x") {
		t.Errorf("the hold carried an escape sequence or lost the error: %q", body)
	}
}
