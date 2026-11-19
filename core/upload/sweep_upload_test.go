package upload

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"testing"
)

func TestSweepSpilledPartRemovedAfterRefusal(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	t.Setenv("TMP", tmp)
	t.Setenv("TEMP", tmp)
	if got := os.TempDir(); got != tmp {
		t.Fatalf("os.TempDir() = %q, want %q; spilled parts would not land in the inspected directory", got, tmp)
	}
	h := Handler(Config{MaxSize: 8 << 20, AllowedExts: []string{"png"}, Storage: &recordingStorage{}})
	body, ct := multipartBody(t, 2<<20)
	req := httptest.NewRequest(http.MethodPost, "/upload", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want the extension refusal, got %d", rec.Code)
	}
	if entries, _ := os.ReadDir(tmp); len(entries) != 0 {
		t.Fatalf("a refused upload left %d spilled file(s)", len(entries))
	}
}

func TestSweepSanitizeBoundsWork(t *testing.T) {
	huge := strings.Repeat("a.", 4<<20)
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	_ = SanitizeFilename(huge)
	runtime.ReadMemStats(&after)
	if got := after.TotalAlloc - before.TotalAlloc; got > 1<<20 {
		t.Fatalf("SanitizeFilename of an 8 MiB name allocated %d bytes, want the input bounded first", got)
	}
}

func TestSweepMIMEAllowListIgnoresCase(t *testing.T) {
	if err := ValidateMIME(bytes.NewReader([]byte("plain text")), []string{"Text/Plain"}); err != nil {
		t.Fatalf("an allow-list entry in another case was refused: %v", err)
	}
}

func TestSweepSanitizeStripsC1Controls(t *testing.T) {
	got := SanitizeFilename("a\u009b31mb\u0090.txt")
	if strings.ContainsAny(got, "\u009b\u0090") {
		t.Fatalf("SanitizeFilename kept a C1 control: %q", got)
	}
}

func TestSweepResolveRefusesSymlinkedMissingTail(t *testing.T) {
	base, outside := t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, base+"/link"); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	if _, _, err := ResolveUnderRoot(base, base+"/link/new/file.txt"); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("a missing tail under a symlinked directory out of the base resolved: %v", err)
	}
}

type cancelAtEOF struct {
	r      *strings.Reader
	cancel func()
}

func (c *cancelAtEOF) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if err != nil {
		c.cancel()
	}
	return n, err
}

func TestSweepSaveCanceledAfterCopyKeepsObject(t *testing.T) {
	ls := newTestLocalStorage(t, t.TempDir())
	if err := ls.Save(context.Background(), "k.txt", strings.NewReader("old")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := ls.Save(ctx, "k.txt", &cancelAtEOF{r: strings.NewReader("new"), cancel: cancel}); !errors.Is(err, context.Canceled) {
		t.Fatalf("a save canceled before its rename returned %v", err)
	}
	rc, err := ls.Get(context.Background(), "k.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	got, _ := io.ReadAll(rc)
	if string(got) != "old" {
		t.Fatalf("a canceled save replaced the object with %q", got)
	}
}

func TestSweepFoldWalkErrorNamesNoRoot(t *testing.T) {
	base := t.TempDir()
	if err := os.WriteFile(base+"/file.txt", []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := RefuseFoldedKey("file.txt/child", "", base, nil)
	if err == nil {
		if runtime.GOOS == "windows" {
			t.Skip("Windows reports a path under a file as missing, so the walk has no error to scrub")
		}
		t.Fatal("the fold walk under a regular file returned no error, so the scrub was never reached")
	}
	if strings.Contains(err.Error(), base) {
		t.Fatalf("the fold walk's error names the storage root: %v", err)
	}
}
