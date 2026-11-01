package appgen

// cliInFilterProbe joins the generated app's e2e package; TestUnionApp
// writes it into the union app. It boots the app, stores three posts,
// builds the app's generated CLI (cmd/union) and lists through it, so
// the check is the server's reading of the query the CLI sends, not the
// emitted source. The bare --title flag is equality on the whole value
// (the server reads a bare key as OpEq), so --title a,b matches a title
// that is literally "a,b" and nothing else; --title-in a,b sends
// ?title_in=a,b, the IN the help names. The bare flag used to promise
// "comma list = IN" and sent ?title=a,b, which matched no row.
const cliInFilterProbe = `package main

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestCLIInFilter(t *testing.T) {
	t.Setenv("ADMIN_SEED_PASSWORD", "roster-pw-123")
	dir := t.TempDir()
	bin, cli := filepath.Join(dir, "app"), filepath.Join(dir, "union-cli")
	for out, pkg := range map[string]string{bin: ".", cli: "./cmd/union"} {
		if b, err := exec.Command("go", "build", "-o", out, pkg).CombinedOutput(); err != nil {
			t.Fatalf("build %s: %v\n%s", pkg, err, b)
		}
	}
	addr := e2eFreeAddr(t)
	srv := exec.Command(bin)
	srv.Dir = dir
	srv.Env = append(os.Environ(), "PORT="+addr, "DATABASE_URL=file:"+filepath.Join(dir, "e2e.db"))
	e2eCaptureServerLog(t, srv, dir)
	if err := srv.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = srv.Process.Kill(); _, _ = srv.Process.Wait() })
	base := "http://" + addr
	e2eWaitReady(t, base)

	jar, _ := cookiejar.New(nil)
	ann := &http.Client{Jar: jar, Timeout: 10 * time.Second}
	creds := url.Values{"email": {"ann@example.com"}, "password": {"roster-pw-123"}}
	if _, err := ann.PostForm(base+"/account/register", creds); err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, err := ann.PostForm(base+"/account/login", creds); err != nil {
		t.Fatalf("login: %v", err)
	}
	for _, title := range []string{"a", "b", "c", "a,b"} {
		body, _ := json.Marshal(map[string]string{"title": title, "status": "draft"})
		if code, out := e2eDo(t, ann, "POST", base+"/api/posts", string(body)); code != http.StatusCreated {
			t.Fatalf("POST /api/posts %q = %d: %s", title, code, out)
		}
	}

	home := t.TempDir()
	list := func(args ...string) []string {
		t.Helper()
		cmd := exec.Command(cli, append([]string{"posts", "list", "--url", base, "--limit", "50"}, args...)...)
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "XDG_CONFIG_HOME=" + filepath.Join(home, ".config")}
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("union posts list %v: %v\n%s", args, err, out)
		}
		var resp struct {
			Data []struct {
				Title string ` + "`" + `json:"title"` + "`" + `
			} ` + "`" + `json:"data"` + "`" + `
		}
		if err := json.Unmarshal(out, &resp); err != nil {
			t.Fatalf("decode union posts list %v: %v\n%s", args, err, out)
		}
		var titles []string
		for _, r := range resp.Data {
			titles = append(titles, r.Title)
		}
		slices.Sort(titles)
		return titles
	}
	for _, c := range []struct {
		args []string
		want []string
	}{
		{[]string{"--title-in", "a,b"}, []string{"a", "b"}},
		{[]string{"--title-in", "c"}, []string{"c"}},
		{[]string{"--title", "a,b"}, []string{"a,b"}},
		{[]string{"--title", "a"}, []string{"a"}},
	} {
		if got := list(c.args...); !slices.Equal(got, c.want) {
			t.Errorf("union posts list %v listed titles %q, want %q", c.args, got, c.want)
		}
	}
}
`
