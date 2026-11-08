package releasecmd

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"regexp"
	"strings"
	"text/template"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appgen"
)

//go:embed install.sh.tmpl
var installShTmpl string

//go:embed install.ps1.tmpl
var installPs1Tmpl string

var (
	shTemplate  = template.Must(template.New("install.sh").Parse(installShTmpl))
	ps1Template = template.Must(template.New("install.ps1").Parse(installPs1Tmpl))
)

// scriptBuild is one archive as the install scripts see it.
type scriptBuild struct {
	Platform string // "linux/amd64"
	Arch     string // "amd64"
	File     string
	SHA256   string
}

// scriptValues fill the install-script templates. Every string lands
// inside a single-quoted sh or PowerShell literal, so checkScriptValues
// holds each one to a character set with no quote, no expansion
// character and no whitespace before a template sees it.
type scriptValues struct {
	Name      string
	Version   string
	BaseURL   string
	EnvPrefix string
	Service   bool
	// DarwinArm64 is a darwin/arm64 build in the release: a Rosetta
	// shell on Apple silicon gets it instead of the amd64 one.
	DarwinArm64 bool
	Unix        []scriptBuild
	Windows     []scriptBuild
}

// envPrefix is the binary name as an environment-variable prefix,
// the one a generated CLI uses for <BINARY>_URL: upper case, every
// character other than a letter or digit turned into "_", outer "_"
// trimmed.
func envPrefix(name string) string { return appgen.CLIEnvPrefix(name) }

func scriptData(res result, opts options) scriptValues {
	v := scriptValues{
		Name:      res.name,
		Version:   opts.version,
		BaseURL:   opts.baseURL,
		EnvPrefix: envPrefix(res.name),
		Service:   opts.service,
	}
	for _, a := range res.artifacts {
		b := scriptBuild{Platform: a.target.String(), Arch: a.target.goarch, File: a.file, SHA256: a.sha256}
		v.DarwinArm64 = v.DarwinArm64 || a.target == target{"darwin", "arm64"}
		if a.target.goos == "windows" {
			v.Windows = append(v.Windows, b)
		} else {
			v.Unix = append(v.Unix, b)
		}
	}
	return v
}

var (
	envPrefixRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
	sha256Re    = regexp.MustCompile(`^[0-9a-f]{64}$`)
	fileRe      = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
)

// checkScriptValues refuses a value the templates cannot embed safely.
// parseOptions has already checked each one; this keeps the emitter safe
// on its own.
func checkScriptValues(v scriptValues) error {
	ok := nameRe.MatchString(v.Name) &&
		versionRe.MatchString(v.Version) &&
		urlCharsRe.MatchString(v.BaseURL) && strings.HasPrefix(v.BaseURL, "https://") &&
		envPrefixRe.MatchString(v.EnvPrefix)
	for _, b := range append(append([]scriptBuild{}, v.Unix...), v.Windows...) {
		ok = ok && fileRe.MatchString(b.File) && sha256Re.MatchString(b.SHA256) &&
			fileRe.MatchString(b.Arch) && strings.Count(b.Platform, "/") == 1 && fileRe.MatchString(strings.Replace(b.Platform, "/", "", 1))
	}
	if !ok {
		return fmt.Errorf("release: refusing to write install scripts from a value outside the safe character set")
	}
	return nil
}

// writeScripts writes install.sh (when the release has a macOS or Linux
// build) and install.ps1 (when it has a Windows build) and returns the
// names written.
func writeScripts(root *os.Root, v scriptValues) ([]string, error) {
	if err := checkScriptValues(v); err != nil {
		return nil, err
	}
	var written []string
	render := func(t *template.Template, name string, mode os.FileMode, crlf bool) error {
		var buf bytes.Buffer
		if err := t.Execute(&buf, v); err != nil {
			return fmt.Errorf("release: %s: %v", name, err)
		}
		data := buf.Bytes()
		if crlf {
			data = bytes.ReplaceAll(data, []byte("\n"), []byte("\r\n"))
		}
		if err := writeNew(root, name, data, mode); err != nil {
			return err
		}
		written = append(written, name)
		return nil
	}
	if len(v.Unix) > 0 {
		//gofastr:allow(worldreadable) the install script is a public download meant to be run, holds no state or secret
		if err := render(shTemplate, "install.sh", 0o755, false); err != nil {
			return nil, err
		}
	}
	if len(v.Windows) > 0 {
		//gofastr:allow(worldreadable) the install script is a public download, holds no state or secret
		if err := render(ps1Template, "install.ps1", 0o644, true); err != nil {
			return nil, err
		}
	}
	return written, nil
}
