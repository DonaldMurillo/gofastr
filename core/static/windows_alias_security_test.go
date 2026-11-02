package static

import (
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"
)

// windowsFS resolves names the way an *os.Root does on Windows, which is
// the open every disk-backed request goes through there: a backslash is a
// path separator, GetFullPathName folds "a\..\b" to "b" lexically and
// strips trailing dots and spaces from each component, and NTFS opens a
// file's default data stream for "name::$DATA". Lookups are
// case-insensitive, as on NTFS. It lets the refusals be proven on any OS
// against the resolution Windows actually performs.
type windowsFS struct{ files fstest.MapFS }

func (w windowsFS) Open(name string) (fs.File, error) {
	segs := strings.FieldsFunc(name, func(r rune) bool { return r == '/' || r == '\\' })
	for i, s := range segs {
		if j := strings.IndexByte(s, ':'); j >= 0 {
			s = s[:j]
		}
		if s != "." && s != ".." {
			s = strings.TrimRight(s, ". ")
		}
		segs[i] = s
	}
	clean := strings.TrimPrefix(path.Clean("/"+strings.Join(segs, "/")), "/")
	if clean == "" {
		clean = "."
	}
	for k := range w.files {
		if strings.EqualFold(k, clean) {
			return w.files.Open(k)
		}
	}
	return w.files.Open(clean)
}

// TestStaticRefusesWindowsPathAliases: on Windows the dot-segment and
// config-file refusals read the request path split on "/" alone, while
// the open resolves "\" as a separator and drops trailing dots, spaces
// and an ::$DATA stream suffix. GET /assets%5C..%5C.env named no dot
// segment to the check and opened .env; /web.config. and
// /web.config::$DATA opened web.config. Every refusal must judge the
// name the open will resolve.
func TestStaticRefusesWindowsPathAliases(t *testing.T) {
	files := fstest.MapFS{
		".env":           {Data: []byte("SECRET_KEY=abc123")},
		".git/config":    {Data: []byte("[remote]")},
		"web.config":     {Data: []byte("<configuration/>")},
		"assets/app.css": {Data: []byte("body{}")},
		"index.html":     {Data: []byte("home")},
	}
	h := Handler(Config{FS: windowsFS{files}})

	// The emulation must actually reach the secret, or the refusals
	// below prove nothing.
	if f, err := (windowsFS{files}).Open(`assets\..\.env`); err != nil {
		t.Fatalf("emulated Windows open of assets\\..\\.env: %v", err)
	} else {
		f.Close()
	}
	if rr := doGet(h, "/assets/app.css"); rr.Code != http.StatusOK {
		t.Fatalf("plain file: status %d, want 200", rr.Code)
	}

	for _, p := range []string{
		"/assets%5C..%5C.env",
		"/assets%5C.env",
		"/x%5C..%5C.git%5Cconfig",
		"/web.config.",
		"/web.config%20",
		"/Web.Config::$DATA",
		"/assets%5C..%5Cweb.config",
		"/assets%5C..%5C..%5C.env",
	} {
		if rr := doGet(h, p); rr.Code != http.StatusNotFound {
			t.Errorf("GET %s: status %d (body %q), want 404: the name resolves to a refused file on Windows", p, rr.Code, rr.Body.String())
		}
	}
}

// TestRefusesWindowsPathAliases pins the exported predicate the UI host
// and the static export share: it must refuse the same Windows spellings
// the handler does.
func TestRefusesWindowsPathAliases(t *testing.T) {
	for _, p := range []string{
		`assets\..\.env`, `a\.git\config`, `web.config.`, `web.config `,
		`Web.Config::$DATA`, `sub\web.config`, `sub/Global.asax...`,
	} {
		if !Refuses(p) {
			t.Errorf("Refuses(%q) = false, want true", p)
		}
	}
	for _, p := range []string{"assets/app.css", "a.b/c.d", "index.html", "web.configs"} {
		if Refuses(p) {
			t.Errorf("Refuses(%q) = true, want false", p)
		}
	}
}

// shortNameFS adds NTFS 8.3 short names to windowsFS: a volume with 8.3
// generation on opens WEB~1.CON as web.config and ENV~1 as .env (the
// leading dot is dropped from a short name), so a refusal that reads the
// long names never sees the request.
type shortNameFS struct {
	windowsFS
	short map[string]string
}

func (s shortNameFS) Open(name string) (fs.File, error) {
	segs := strings.Split(name, "/")
	for i, seg := range segs {
		for sn, long := range s.short {
			if strings.EqualFold(seg, sn) {
				segs[i] = long
			}
		}
	}
	return s.windowsFS.Open(strings.Join(segs, "/"))
}

// TestStaticRefusesWindowsShortNames: on a Windows volume that generates
// 8.3 names, GET /WEB~1.CON served web.config and /ENV~1 served .env past
// the config and dotfile refusals, which judge the spelled name. A segment
// holding "~" then a digit is the short-name shape, refused on every OS
// as the backslash split is, so every platform gives one answer.
func TestStaticRefusesWindowsShortNames(t *testing.T) {
	files := fstest.MapFS{
		".env":           {Data: []byte("SECRET_KEY=abc123")},
		".git/config":    {Data: []byte("[remote]")},
		"web.config":     {Data: []byte("<configuration/>")},
		"assets/app.css": {Data: []byte("body{}")},
		"a~b.txt":        {Data: []byte("tilde")},
	}
	sfs := shortNameFS{windowsFS{files}, map[string]string{"WEB~1.CON": "web.config", "ENV~1": ".env", "GIT~1": ".git"}}
	if f, err := sfs.Open("ENV~1"); err != nil {
		t.Fatalf("setup: the emulated short name does not reach .env: %v", err)
	} else {
		f.Close()
	}
	h := Handler(Config{FS: sfs})
	for _, p := range []string{"/WEB~1.CON", "/web~1.con", "/ENV~1", "/GIT~1/config", "/assets/..%5CWEB~1.CON"} {
		if rr := doGet(h, p); rr.Code != http.StatusNotFound {
			t.Errorf("GET %s: status %d (body %q), want 404: the short name opens a refused file on Windows", p, rr.Code, rr.Body.String())
		}
	}
	for _, p := range []string{"/assets/app.css", "/a~b.txt"} {
		if rr := doGet(h, p); rr.Code != http.StatusOK {
			t.Errorf("GET %s: status %d, want 200: a ~ not followed by a digit is no short name", p, rr.Code)
		}
	}

	for _, p := range []string{`WEB~1.CON`, `sub/ENV~1`, `GIT~12\config`, `x/PROGRA~1`} {
		if !Refuses(p) {
			t.Errorf("Refuses(%q) = false, want true", p)
		}
	}
	for _, p := range []string{"a~b.txt", "notes.txt~", "~/x.css", "v1.2/app.css"} {
		if Refuses(p) {
			t.Errorf("Refuses(%q) = true, want false", p)
		}
	}
}

// TestStaticRefusesBackslashDotfileOnDisk runs the same request against a
// real directory through *os.Root. It is the end-to-end proof on Windows;
// elsewhere a backslash is an ordinary byte and the open misses anyway.
func TestStaticRefusesBackslashDotfileOnDisk(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := Handler(Config{FS: os.DirFS(dir)})
	if rr := doGet(h, "/assets%5C..%5C.env"); rr.Code != http.StatusNotFound {
		t.Errorf("GET /assets%%5C..%%5C.env on %s: status %d, want 404", runtime.GOOS, rr.Code)
	}
}
