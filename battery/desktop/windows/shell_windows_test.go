//go:build windows && amd64

package windows

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestWebviewUserDataDirUsesApplicationID(t *testing.T) {
	base := t.TempDir()
	first := webviewUserDataDirAt(base, "notes.example.app", "Same Title")
	second := webviewUserDataDirAt(base, "calendar.example.app", "Same Title")
	if first == second {
		t.Fatal("different app IDs share a WebView2 profile")
	}
	if got := webviewUserDataDirAt(base, "notes.example.app", "Different Title"); got != first {
		t.Fatalf("profile changed with title: %q, want %q", got, first)
	}
	if filepath.Base(first) != "WebView2" || filepath.Base(filepath.Dir(first)) == "Same Title" {
		t.Fatalf("profile path is not app-ID based: %q", first)
	}
}

func TestWebviewUserDataDirUsesTitleWhenNoApplicationID(t *testing.T) {
	base := t.TempDir()
	if got, want := webviewUserDataDirAt(base, "", "Same Title"), filepath.Join(base, "gofastr", "Same-Title", "WebView2"); got != want {
		t.Fatalf("fallback profile path = %q, want %q", got, want)
	}
}

func TestWebView2LoaderCandidatesExcludeWorkingDirectory(t *testing.T) {
	executable := filepath.Join("C:", "apps", "gofastr", "app.exe")
	want := []string{filepath.Join("C:", "override", "WebView2Loader.dll"), filepath.Join("C:", "apps", "gofastr", "WebView2Loader.dll")}
	if got := webView2LoaderCandidates(filepath.Join("C:", "override", "WebView2Loader.dll"), executable); !reflect.DeepEqual(got, want) {
		t.Fatalf("loader candidates = %v, want override and executable directory %v", got, want)
	}
	if got := webView2LoaderCandidates("", ""); len(got) != 0 {
		t.Fatalf("loader candidates without explicit paths = %v, want none", got)
	}
}
