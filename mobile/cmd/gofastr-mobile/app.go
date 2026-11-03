package main

import (
	"bytes"
	"embed"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
)

//go:embed templates
var templates embed.FS

// The bind package's contract: gomobile binds the package named by --pkg,
// and the generated hosts call exactly these functions on it, so any app
// that exports them (examples/notes/notesmobile is the reference) runs:
//
//	func Start(dataDir, origin string) error
//	func Stop() error
//	func Handle(method, url, headers string, body []byte) (*Response, error)
//	func Open(method, url, headers string, body []byte) (*Stream, error)
//	type Response struct{ Status int; Headers string; Body []byte }
//	type Stream  struct{ Status int; Headers string } // + Read(max int) ([]byte, error), Close() error

const (
	iosScheme     = "gofastr"
	iosOrigin     = "gofastr://app"
	androidHost   = "appassets.androidplatform.net"
	androidOrigin = "https://" + androidHost
	minIOS        = "16.0" // NSLock.withLock, WKWebView.isInspectable checks
	minSDK        = 24
)

// app is everything the templates and steps need to know about the app.
type app struct {
	PkgPath string // import path gomobile binds
	PkgName string // Go package name

	Prefix    string // Objective-C/Swift name prefix gomobile derives (Notesmobile)
	JavaPkg   string // Java package of the bindings (notesmobile)
	JavaClass string // Java class holding the package functions (Notesmobile)

	ID      string // bundle id / application id
	Name    string // what the home screen shows
	Exe     string // iOS bundle executable
	Version string
	Build   string

	VersionCode int
	MinOS       string
	MinSDK      int
	TargetSDK   int

	Scheme string
	Origin string
	Host   string
}

var (
	goIdent   = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	appIDRe   = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]*(\.[a-zA-Z][a-zA-Z0-9_]*)+$`)
	appNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._-]{0,29}$`)
	versionRe = regexp.MustCompile(`^[0-9]+(\.[0-9]+){0,2}$`)
)

// javaReserved are the Java keywords an application id segment may not
// be: the id doubles as the generated Activity's Java package.
var javaReserved = map[string]bool{
	"abstract": true, "assert": true, "boolean": true, "break": true, "byte": true, "case": true,
	"catch": true, "char": true, "class": true, "const": true, "continue": true, "default": true,
	"do": true, "double": true, "else": true, "enum": true, "extends": true, "final": true,
	"finally": true, "float": true, "for": true, "goto": true, "if": true, "implements": true,
	"import": true, "instanceof": true, "int": true, "interface": true, "long": true, "native": true,
	"new": true, "package": true, "private": true, "protected": true, "public": true, "return": true,
	"short": true, "static": true, "strictfp": true, "super": true, "switch": true, "synchronized": true,
	"this": true, "throw": true, "throws": true, "transient": true, "try": true, "void": true,
	"volatile": true, "true": true, "false": true, "null": true,
}

// newApp validates the inputs that land in generated source and derives
// the names gomobile will generate. Every value that reaches a template
// is checked against a grammar here, so the templates never quote.
func newApp(pkgPath, pkgName, id, name, version string) (app, error) {
	if !goIdent.MatchString(pkgName) {
		return app{}, fmt.Errorf("package name %q: gomobile needs a lowercase Go identifier", pkgName)
	}
	if id == "" {
		id = "dev.gofastr." + pkgName
	}
	if !appIDRe.MatchString(id) {
		return app{}, fmt.Errorf("--id %q: want a reverse-DNS id such as dev.example.notes (letters, digits, _; dot-separated)", id)
	}
	for _, seg := range strings.Split(id, ".") {
		if javaReserved[seg] {
			return app{}, fmt.Errorf("--id %q: segment %q is a Java keyword, and the id is the Android Activity's package", id, seg)
		}
	}
	if name == "" {
		name = strings.ToUpper(pkgName[:1]) + pkgName[1:]
	}
	if !appNameRe.MatchString(name) {
		return app{}, fmt.Errorf("--name %q: 1-30 letters, digits, spaces, . _ -", name)
	}
	if version == "" {
		version = "0.1.0"
	}
	if !versionRe.MatchString(version) {
		return app{}, fmt.Errorf("--version %q: want N, N.N or N.N.N", version)
	}
	prefix := strings.ToUpper(pkgName[:1]) + pkgName[1:]
	return app{
		PkgPath:     pkgPath,
		PkgName:     pkgName,
		Prefix:      prefix,
		JavaPkg:     pkgName,
		JavaClass:   prefix,
		ID:          id,
		Name:        name,
		Exe:         prefix,
		Version:     version,
		Build:       "1",
		VersionCode: 1,
		MinOS:       minIOS,
		MinSDK:      minSDK,
		TargetSDK:   minSDK,
	}, nil
}

func (a app) forIOS() app {
	a.Scheme, a.Origin, a.Host = iosScheme, iosOrigin, "app"
	return a
}

func (a app) forAndroid(targetSDK int) app {
	a.Scheme, a.Origin, a.Host = "https", androidOrigin, androidHost
	if targetSDK > a.MinSDK {
		a.TargetSDK = targetSDK
	}
	return a
}

// render fills one embedded template.
func render(path string, a app) ([]byte, error) {
	src, err := templates.ReadFile(path)
	if err != nil {
		return nil, err
	}
	t, err := template.New(filepath.Base(path)).Option("missingkey=error").Funcs(template.FuncMap{
		"xml": func(s string) (string, error) {
			var b bytes.Buffer
			if err := xml.EscapeText(&b, []byte(s)); err != nil {
				return "", err
			}
			return b.String(), nil
		},
	}).Parse(string(src))
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := t.Execute(&out, a); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// writeRendered renders a template to dst, creating its directory.
func writeRendered(tmpl, dst string, a app) error {
	b, err := render(tmpl, a)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	return writeOwnerOnly(dst, b)
}

// writeOwnerOnly writes b to path as 0600 even when path already exists
// with a wider mode (os.WriteFile's mode applies only at create).
// PROTOTYPE FINDING: this is internal/fileperm.WriteOwnerOnly, which a
// module outside github.com/DonaldMurillo/gofastr cannot import.
func writeOwnerOnly(path string, b []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// resolvePkg asks the go tool which package --pkg names, so a relative
// directory and an import path both work.
func resolvePkg(r *runner, pkg string) (path, name string, err error) {
	// Resolved even in a dry run: it is read-only and the plan needs the
	// real names.
	dry := r.dryRun
	r.dryRun = false
	defer func() { r.dryRun = dry }()
	out, err := r.output("resolve the bind package", "", "go", "list", "-f", "{{.ImportPath}} {{.Name}}", pkg)
	if err != nil {
		return "", "", err
	}
	path, name, ok := strings.Cut(out, " ")
	if !ok || strings.Contains(out, "\n") {
		return "", "", fmt.Errorf("--pkg %q must name exactly one package (go list said %q)", pkg, out)
	}
	return path, name, nil
}

// bootstrapGomobile builds the gomobile and gobind the module pins (the
// go.mod tool directive) into bin and puts bin first on PATH, so nothing
// is installed globally and every run uses the same version.
func bootstrapGomobile(r *runner, bin string) error {
	if err := r.do("create the tools directory", func() error { return os.MkdirAll(bin, 0o700) }); err != nil {
		return err
	}
	for _, tool := range []string{"gomobile", "gobind"} {
		if err := r.run("build "+tool+" at the version go.mod pins", "go", "build", "-o", filepath.Join(bin, tool), "golang.org/x/mobile/cmd/"+tool); err != nil {
			return fmt.Errorf("%w\n    (the module running gofastr-mobile must require golang.org/x/mobile: go get -tool golang.org/x/mobile/cmd/gomobile golang.org/x/mobile/cmd/gobind)", err)
		}
	}
	r.env = append(r.env, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return nil
}

var errUsage = errors.New("usage")
