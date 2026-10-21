package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/examples/meridian/entities/client"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// TestUploadRoundTrip sends a customer's logo through each generated
// client (the Go client, the meridian CLI and the JS SDK in Chrome) to
// the running app, then reads every stored file back and compares bytes.
//
//gofastr:test-reads /cmd/gofastr/
func TestUploadRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("builds + boots the binary")
	}
	bin := t.TempDir()
	gofastr := filepath.Join(bin, "gofastr")
	meridian := filepath.Join(bin, "meridian")
	for out, pkg := range map[string]string{gofastr: "../../cmd/gofastr", meridian: "./cmd/meridian"} {
		build := exec.Command("go", "build", "-o", out, pkg)
		build.Stderr = os.Stderr
		if err := build.Run(); err != nil {
			t.Fatalf("build %s: %v", pkg, err)
		}
	}
	// The JS SDK is served from gen/sdk/dist under the app's working
	// directory, the way `gofastr generate sdk` lays it out.
	base := e2eBootAppIn(t, func(dir string) {
		if err := os.CopyFS(filepath.Join(dir, "entities"), os.DirFS("entities")); err != nil {
			t.Fatal(err)
		}
		gen := exec.Command(gofastr, "generate", "sdk", "--target", "js", "--name", "meridian", "--out", "gen/sdk")
		gen.Dir = dir
		if out, err := gen.CombinedOutput(); err != nil {
			t.Fatalf("generate sdk: %v\n%s", err, out)
		}
	})
	session, token := uploadSession(t, base)
	logo := uploadPNG(t)
	stored := func(t *testing.T, key string) []byte {
		t.Helper()
		if key == "" {
			t.Fatal("the customer came back with no logo")
		}
		resp, err := session.Get(base + filesURL + key)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s = %d: %s", key, resp.StatusCode, body)
		}
		return body
	}

	t.Run("go", func(t *testing.T) {
		c := client.NewClient(base+"/api", nil)
		c.Token = token
		ctx := context.Background()
		created, err := c.CreateCustomersWithFiles(ctx, client.CustomersInput{Name: "Go Upload", Email: "go-upload@example.com"},
			client.CustomersFiles{Logo: &client.FilePart{Name: "acme.png", Body: bytes.NewReader(logo)}})
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if created.Name != "Go Upload" || !bytes.Equal(stored(t, created.Logo), logo) {
			t.Fatalf("create stored %+v", created)
		}
		// A patch sends only what it sets: the name survives a new logo.
		other := uploadPNGColor(t, color.RGBA{B: 255, A: 255})
		patched, err := c.PatchCustomersWithFiles(ctx, created.ID, client.CustomersPatch{Company: new("Acme")},
			client.CustomersFiles{Logo: &client.FilePart{Name: "acme-blue.png", Body: bytes.NewReader(other)}})
		if err != nil {
			t.Fatalf("patch: %v", err)
		}
		if patched.Name != "Go Upload" || patched.Company != "Acme" || !bytes.Equal(stored(t, patched.Logo), other) {
			t.Fatalf("patch stored %+v", patched)
		}
	})

	t.Run("cli", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "acme.png")
		if err := os.WriteFile(path, logo, 0o600); err != nil {
			t.Fatal(err)
		}
		run := func(stdin io.Reader, args ...string) map[string]any {
			t.Helper()
			cmd := exec.Command(meridian, args...)
			cmd.Env = append(os.Environ(), "HOME="+dir, "XDG_CONFIG_HOME="+dir, "MERIDIAN_URL="+base, "MERIDIAN_TOKEN="+token)
			cmd.Stdin = stdin
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("meridian %s: %v\n%s", strings.Join(args, " "), err, stderr.String())
			}
			var row map[string]any
			if err := json.Unmarshal(out, &row); err != nil {
				t.Fatalf("meridian output: %v\n%s", err, out)
			}
			return row
		}
		row := run(nil, "customers", "create", "--name", "CLI Upload", "--email", "cli-upload@example.com", "--logo", path)
		key, _ := row["logo"].(string)
		if !bytes.Equal(stored(t, key), logo) {
			t.Fatalf("create from a path stored %v", row)
		}
		id, _ := row["id"].(string)
		other := uploadPNGColor(t, color.RGBA{G: 255, A: 255})
		row = run(bytes.NewReader(other), "customers", "patch", id, "--logo", "-")
		key, _ = row["logo"].(string)
		if row["name"] != "CLI Upload" || !bytes.Equal(stored(t, key), other) {
			t.Fatalf("patch from stdin stored %v", row)
		}
	})

	t.Run("js", func(t *testing.T) {
		ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
		scenario := `(async () => {
  const { Client } = await import('/docs/api/sdk/client.js');
  const api = new Client({ baseURL: location.origin + '/api', token: ` + jsString(token) + ` });
  const bytes = Uint8Array.from(atob(` + jsString(base64.StdEncoding.EncodeToString(logo)) + `), (c) => c.charCodeAt(0));
  const created = await api.customers.create({
    name: 'JS Upload', email: 'js-upload@example.com',
    logo: new File([bytes], 'acme.png', { type: 'image/png' }),
  });
  window.__result = { name: created.name, logo: created.logo };
})().catch((e) => { window.__result = { fatal: String((e && e.stack) || e) }; });
true`
		var result map[string]any
		if err := chromedp.Run(ctx,
			chromedp.Navigate(base+"/docs/api"),
			chromedp.WaitReady("body", chromedp.ByQuery),
			chromedp.Evaluate(scenario, nil),
			chromedp.Poll(`window.__result`, &result, chromedp.WithPollingTimeout(30*time.Second)),
		); err != nil {
			t.Fatalf("browser: %v", err)
		}
		if fatal, ok := result["fatal"]; ok {
			t.Fatalf("scenario failed in the browser: %v", fatal)
		}
		key, _ := result["logo"].(string)
		if result["name"] != "JS Upload" || !bytes.Equal(stored(t, key), logo) {
			t.Fatalf("create stored %v", result)
		}
	})
}

// uploadSession signs the seeded admin in and mints an API token scoped
// to customers. The session reads stored files back; the token writes.
func uploadSession(t *testing.T, base string) (*http.Client, string) {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	session := &http.Client{Jar: jar}
	resp, err := session.PostForm(base+"/auth/login", url.Values{"email": {"admin@meridian.dev"}, "password": {os.Getenv("ADMIN_SEED_PASSWORD")}})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	resp.Body.Close()
	code, body := e2eDo(t, session, "POST", base+"/auth/tokens", `{"name":"upload-e2e","scopes":["customers:*"]}`)
	if code < 200 || code >= 300 {
		t.Fatalf("mint token = %d: %s", code, body)
	}
	var minted struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal([]byte(body), &minted); err != nil || minted.Token == "" {
		t.Fatalf("mint response: %v %s", err, body)
	}
	return session, minted.Token
}

// uploadPNG is a small red PNG, the logo every client sends.
func uploadPNG(t *testing.T) []byte {
	return uploadPNGColor(t, color.RGBA{R: 255, A: 255})
}

func uploadPNGColor(t *testing.T, c color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for x := range 4 {
		for y := range 4 {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// jsString quotes s as a JS string literal.
func jsString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
