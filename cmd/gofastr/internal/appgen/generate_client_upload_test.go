package appgen

import (
	"go/format"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/framework"
)

// uploadDecl is a customers entity with an optional Image field, a
// required File field, a read-only Image field, a JSON field and a
// snake_case column, the shapes the upload calls have to map.
func uploadDecl() framework.EntityDeclaration {
	tsOff := false
	return framework.EntityDeclaration{
		Name: "customers", Table: "customers", Timestamps: &tsOff,
		Fields: []framework.FieldDeclaration{
			{Name: "name", Type: "string", Required: true},
			{Name: "plan_tier", Type: "string"},
			{Name: "logo", Type: "image"},
			{Name: "contract", Type: "file", Required: true},
			{Name: "meta", Type: "json"},
			{Name: "preview", Type: "image", ReadOnly: true},
			{Name: "secret_scan", Type: "file", Hidden: true},
		},
	}
}

func TestClientUploadCalls(t *testing.T) {
	src := renderClient([]framework.EntityDeclaration{uploadDecl()})
	formatted, err := format.Source([]byte(src))
	if err != nil {
		t.Fatalf("client does not parse: %v\n%s", err, src)
	}
	out := string(formatted)
	for _, want := range []string{
		"type FilePart struct",
		"func (c *Client) DoMultipart(",
		"type CustomersFiles struct",
		"\tLogo     *FilePart",
		"\tContract *FilePart",
		`"planTier": "plan_tier"`,
		"func (c *Client) CreateCustomersWithFiles(ctx context.Context, body CustomersInput, files CustomersFiles) (Customers, error)",
		"func (c *Client) UpdateCustomersWithFiles(ctx context.Context, id string, body CustomersInput, files CustomersFiles) (Customers, error)",
		"func (c *Client) PatchCustomersWithFiles(ctx context.Context, id string, body CustomersPatch, files CustomersFiles) (Customers, error)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("client missing %q", want)
		}
	}
	for _, slot := range []string{"SecretScan", "Preview"} {
		if regexp.MustCompile(`\b` + slot + `\s+\*FilePart`).MatchString(out) {
			t.Errorf("a hidden or read-only file field must not get an upload slot: %s", slot)
		}
	}
}

func TestClientWithoutUploadsHasNoMultipart(t *testing.T) {
	decl := uploadDecl()
	decl.Fields = slices.DeleteFunc(decl.Fields, func(f framework.FieldDeclaration) bool { return f.Type == "image" || f.Type == "file" })
	out := renderClient([]framework.EntityDeclaration{decl})
	for _, gone := range []string{"mime/multipart", "FilePart", "WithFiles"} {
		if strings.Contains(out, gone) {
			t.Errorf("an entity with no Image or File field emits %q", gone)
		}
	}
}

// TestClientUploadGuards compiles the generated client into a temp module
// and runs uploadGuardSource against it: every refusal fires before a
// request leaves, and a good write sends each part under its column.
func TestClientUploadGuards(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles the generated client")
	}
	repoRoot, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	goVersion, err := clitest.RepoGoVersion(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	pkg := filepath.Join(dir, "client")
	if err := os.MkdirAll(pkg, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		filepath.Join(dir, "go.mod"):        "module example.com/up\n\ngo " + goVersion + "\n",
		filepath.Join(pkg, "client.go"):     renderClient([]framework.EntityDeclaration{uploadDecl()}),
		filepath.Join(pkg, "guard_test.go"): uploadGuardSource,
	} {
		if err := os.WriteFile(name, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "./client/")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated client guards: %v\n%s", err, out)
	}
}

const uploadGuardSource = `package client

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

type zeros struct{}

func (zeros) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

func png(n int64) *FilePart {
	return &FilePart{Name: "logo.png", Body: io.LimitReader(zeros{}, n)}
}

func TestGuards(t *testing.T) {
	var hits atomic.Int32
	parts := map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		mr, err := r.MultipartReader()
		if err != nil {
			t.Errorf("not multipart: %v", err)
			return
		}
		for {
			p, err := mr.NextPart()
			if err != nil {
				break
			}
			body, _ := io.ReadAll(p)
			parts[p.FormName()] = string(body)
			if len(body) > 64 {
				parts[p.FormName()] = fmt.Sprintf("<%d bytes>", len(body))
			}
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, ` + "`" + `{"data":{"id":"c1","name":"Acme"}}` + "`" + `)
	}))
	defer srv.Close()
	c := NewClient(srv.URL, nil)
	ctx := context.Background()

	for name, tc := range map[string]struct {
		body  CustomersInput
		files CustomersFiles
		want  string
	}{
		"set and sent":  {CustomersInput{Name: "a", Logo: new("old.png")}, CustomersFiles{Logo: png(3)}, "not both"},
		"file too big":  {CustomersInput{Name: "a"}, CustomersFiles{Logo: png(maxFileBytes + 1)}, "per file"},
		"body too big":  {CustomersInput{Name: "a"}, CustomersFiles{Logo: png(maxFileBytes), Contract: png(maxFileBytes)}, "request is over"},
		"control name":  {CustomersInput{Name: "a"}, CustomersFiles{Logo: &FilePart{Name: "a\r\nb.png", Body: strings.NewReader("x")}}, "control character"},
		"no body":       {CustomersInput{Name: "a"}, CustomersFiles{Logo: &FilePart{Name: "a.png"}}, "has no Body"},
	} {
		_, err := c.CreateCustomersWithFiles(ctx, tc.body, tc.files)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
	}
	if err := c.DoMultipart(ctx, "POST", "/customers", map[string]any{"na\nme": "x"}, nil, nil); err == nil || !strings.Contains(err.Error(), "control character") {
		t.Errorf("control key: err = %v", err)
	}
	// A multipart body has no null: dropping it would leave the column as it was.
	if err := c.DoMultipart(ctx, "PATCH", "/customers/c1", map[string]any{"meta": nil}, map[string]FilePart{"logo": *png(3)}, nil); err == nil || !strings.Contains(err.Error(), "null") {
		t.Errorf("null value: err = %v", err)
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("%d refused writes reached the server", n)
	}

	// Contract is required, so CustomersInput holds it as a plain string:
	// its zero value is no clash with the file sent for it.
	got, err := c.CreateCustomersWithFiles(ctx, CustomersInput{Name: "Acme", PlanTier: new("pro")}, CustomersFiles{
		Logo:     &FilePart{Name: "a.png", Body: strings.NewReader("PNG")},
		Contract: &FilePart{Name: "c.pdf", Body: strings.NewReader("PDF")},
	})
	if err != nil || got.ID != "c1" {
		t.Fatalf("create = %+v, %v", got, err)
	}
	want := map[string]string{"name": "Acme", "plan_tier": "pro", "logo": "PNG", "contract": "PDF"}
	if len(parts) != len(want) {
		t.Errorf("parts = %v, want %v", parts, want)
	}
	for k, v := range want {
		if parts[k] != v {
			t.Errorf("part %s = %q, want %q", k, parts[k], v)
		}
	}
}
`
