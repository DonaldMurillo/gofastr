package themecmd

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

type sendfileRecorder struct {
	*httptest.ResponseRecorder
	readFrom bool
}

func (r *sendfileRecorder) ReadFrom(src io.Reader) (int64, error) {
	r.readFrom = true
	return io.Copy(r.ResponseRecorder, src)
}

// TestFrameWriterKeepsReadFrom: the preview writer hid the host writer's
// ReadFrom, so a file the host served through it lost the sendfile path.
// It forwards ReadFrom, stripping the frame headers first as Write does.
func TestFrameWriterKeepsReadFrom(t *testing.T) {
	inner := &sendfileRecorder{ResponseRecorder: httptest.NewRecorder()}
	fw := &frameFriendlyWriter{rw: inner}
	fw.Header().Set("X-Frame-Options", "DENY")
	if _, err := io.Copy(fw, io.LimitReader(strings.NewReader("x"), 1)); err != nil {
		t.Fatal(err)
	}
	if !inner.readFrom {
		t.Fatal("the body went through Write: ReadFrom (sendfile) was hidden")
	}
	if got := inner.Header().Get("X-Frame-Options"); got != "" {
		t.Fatalf("X-Frame-Options not stripped before the body: %q", got)
	}
}
