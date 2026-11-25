// Package linuxbundle is the Linux desktop bundle: the fixed five-file
// payload `gofastr desktop build --format=tar.gz` writes, its
// manifest, and the installed form `gofastr desktop install` makes of
// it under the user's data directory with an install receipt.
//
// The CLI's install and uninstall and the desktop battery's updater
// all read and write bundles through this package, so a bundle the
// updater swaps in is one uninstall accepts.
package linuxbundle

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/internal/appid"
)

const (
	// ManifestName is the manifest file at a bundle's root.
	ManifestName = "manifest.json"
	// ReceiptName is the install receipt beside the manifest of an installed bundle.
	ReceiptName = ".gofastr-install.json"
	// EntryPoint is the launcher script a bundle runs from.
	EntryPoint = "launch.sh"
	// MaxExecutable is the largest bin/<id> a bundle may carry.
	MaxExecutable = 1 << 30
	// MaxManifest is the largest manifest.json a bundle may carry.
	MaxManifest = 512 << 10
	// MaxReceipt is the largest install receipt read back.
	MaxReceipt = 64 << 10

	// SideSep joins an installation's directory name to "old" and "new"
	// for the copies an update keeps beside it. The app id grammar has no
	// "~", so <id>~old never names another installed app.
	SideSep = "~"

	maxPayloadFile = 16 << 20
	receiptVersion = 1
)

var (
	versionPattern = regexp.MustCompile(VersionGrammar)
	schemePattern  = regexp.MustCompile(SchemeGrammar)
)

const (
	// VersionGrammar is the bundle version grammar, as a regular expression.
	VersionGrammar = `^[0-9]+(\.[0-9A-Za-z]+)*$`
	// SchemeGrammar is the embedded URL scheme grammar, as a regular expression.
	SchemeGrammar = `^[a-z][a-z0-9+.-]{0,31}$`
)

// File is one inventory entry of a bundle manifest.
type File struct {
	Path   string `json:"path"`
	Mode   string `json:"mode"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Manifest is a bundle's manifest.json.
type Manifest struct {
	SchemaVersion  int      `json:"schema_version"`
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Version        string   `json:"version"`
	Platform       string   `json:"platform"`
	EntryPoint     string   `json:"entry_point"`
	EmbeddedScheme string   `json:"embedded_scheme,omitempty"`
	Runtime        []string `json:"runtime"`
	Files          []File   `json:"files"`
}

// Receipt is the install receipt: the digests of the manifest and of the two files install writes outside the bundle directory.
type Receipt struct {
	SchemaVersion  int    `json:"schema_version"`
	ID             string `json:"id"`
	ManifestSHA256 string `json:"manifest_sha256"`
	LauncherSHA256 string `json:"launcher_sha256"`
	DesktopSHA256  string `json:"desktop_sha256"`
}

// NewReceipt is the receipt of an installation of manifest with these launcher and desktop entry bytes.
func NewReceipt(id string, manifest, launcher, desktopEntry []byte) Receipt {
	return Receipt{receiptVersion, id, Digest(manifest), Digest(launcher), Digest(desktopEntry)}
}

// Reissued is r for the same installation holding manifest in place of the one r names.
func (r Receipt) Reissued(manifest []byte) Receipt {
	r.ManifestSHA256 = Digest(manifest)
	return r
}

// ValidID reports whether id is a desktop app id (appid.Check).
func ValidID(id string) bool { return appid.Check(id) == nil }

// ValidName reports whether name is a display name (appid.CheckName) in valid UTF-8, as a desktop entry needs.
func ValidName(name string) bool { return appid.CheckName(name) == nil && utf8.ValidString(name) }

// ValidVersion reports whether version matches VersionGrammar in at most 64 bytes.
func ValidVersion(version string) bool {
	return versionPattern.MatchString(version) && len(version) <= 64
}

// ValidScheme reports whether scheme matches SchemeGrammar.
func ValidScheme(scheme string) bool { return schemePattern.MatchString(scheme) }

// RootName is the archive's single top-level directory: <id>-<version>-linux-<arch>.
func RootName(id, version, arch string) (string, error) {
	if arch != "amd64" && arch != "arm64" {
		return "", fmt.Errorf("unsupported Linux architecture %q", arch)
	}
	root := id + "-" + version + "-linux-" + arch
	if len(root)+len(".tar.gz") > 255 {
		return "", fmt.Errorf("app id and version make the Linux archive filename longer than 255 bytes")
	}
	if root == "." || !fs.ValidPath(root) || strings.ContainsAny(root, "/\\\x00") || path.Clean(root) != root {
		return "", fmt.Errorf("unsafe Linux archive root")
	}
	return root, nil
}

// ParseManifest decodes manifest.json strictly, the manifest and each
// inventory entry refusing unknown or repeated fields, and refuses a
// schema, entry point or platform other than this format's and an id,
// name, version or embedded scheme off their grammars. It checks no
// file: Open does.
func ParseManifest(raw []byte) (Manifest, error) {
	var manifest Manifest
	if err := handler.UnmarshalStrict(raw, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("invalid bundle manifest: %w", err)
	}
	var fields map[string]json.RawMessage
	if err := handler.UnmarshalStrict(raw, &fields); err != nil {
		return Manifest{}, err
	}
	var fileEntries []json.RawMessage
	if err := json.Unmarshal(fields["files"], &fileEntries); err != nil {
		return Manifest{}, err
	}
	for _, rawEntry := range fileEntries {
		var entry File
		if err := handler.UnmarshalStrict(rawEntry, &entry); err != nil {
			return Manifest{}, fmt.Errorf("invalid inventory fields: %w", err)
		}
	}
	if manifest.SchemaVersion != 1 || manifest.EntryPoint != EntryPoint || (manifest.Platform != "linux-amd64" && manifest.Platform != "linux-arm64") {
		return Manifest{}, fmt.Errorf("unsupported bundle schema, entry point, or platform")
	}
	if err := CheckIdentity(manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// CheckIdentity refuses a manifest whose embedded scheme, id, name or
// version is off its grammar, or whose platform and id and version make
// no safe archive root. gofastr desktop build checks its flags through
// it too, so a bundle it writes is one Open accepts.
func CheckIdentity(m Manifest) error {
	if m.EmbeddedScheme != "" && !ValidScheme(m.EmbeddedScheme) {
		return fmt.Errorf("embedded scheme %q must match %s", m.EmbeddedScheme, SchemeGrammar)
	}
	if !ValidID(m.ID) {
		return fmt.Errorf("bundle id %q is not a reverse-DNS app id", m.ID)
	}
	if !ValidName(m.Name) {
		return fmt.Errorf("bundle name must be 1 to 64 bytes of printable UTF-8 without a path separator")
	}
	if !ValidVersion(m.Version) {
		return fmt.Errorf("bundle version must match %s and be at most 64 characters", VersionGrammar)
	}
	_, err := RootName(m.ID, m.Version, strings.TrimPrefix(m.Platform, "linux-"))
	return err
}

// ExpectedFiles is the fixed payload inventory of app id, path to archive mode.
func ExpectedFiles(id string) map[string]string {
	return map[string]string{"README.txt": "0644", "bin/" + id: "0755", id + ".desktop.in": "0644", "icon.png": "0644", EntryPoint: "0755"}
}

// InstalledMode is the owner-only mode an installed copy of a payload entry with archive mode has.
func InstalledMode(mode string) fs.FileMode {
	if mode == "0755" {
		return 0o700
	}
	return 0o600
}

// Digest is the lowercase hex sha256 of data.
func Digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Launcher is the ~/.local/bin script install writes: it execs executable with the arguments unchanged.
func Launcher(executable string) []byte {
	return []byte("#!/bin/sh\nexec " + ShellQuote(executable) + " \"$@\"\n")
}

// ShellQuote quotes value as one POSIX shell word.
func ShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

// EscapeDesktopString applies the Desktop Entry string escapes; '%' and '$' stay literal, as Exec field codes do not apply.
func EscapeDesktopString(value string) string {
	return strings.NewReplacer("\\", "\\\\", " ", "\\s", "\n", "\\n", "\r", "\\r", "\t", "\\t").Replace(value)
}

// DesktopEntry renders the installed desktop entry for manifest, installed at root with its launcher script at launcher.
func DesktopEntry(manifest Manifest, root, launcher string) ([]byte, error) {
	if !textsafe.Storable(launcher) || strings.ContainsAny(launcher, "=\n\r\t") {
		return nil, fmt.Errorf("desktop executable path contains unsupported characters")
	}
	if manifest.EmbeddedScheme != "" && !ValidScheme(manifest.EmbeddedScheme) {
		return nil, fmt.Errorf("embedded scheme %q must match %s", manifest.EmbeddedScheme, SchemeGrammar)
	}
	quoted := `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "`", "\\`", `$`, `\$`, `%`, `%%`).Replace(launcher) + `"`
	// GIO checks Exec's executable before expanding %% into literal %. A
	// fixed shell executable keeps arbitrary installation paths in an argument.
	// There is no -c: the file is our generated launcher; URLs remain argv.
	command := "/bin/sh " + EscapeDesktopString(quoted)
	mime := ""
	if manifest.EmbeddedScheme != "" {
		command += " %u"
		mime = "MimeType=x-scheme-handler/" + manifest.EmbeddedScheme + ";\n"
	}
	return []byte("[Desktop Entry]\nType=Application\nVersion=1.0\nName=" + EscapeDesktopString(manifest.Name) + "\nExec=" + command + "\nIcon=" + EscapeDesktopString(filepath.Join(root, "icon.png")) + "\nTerminal=false\n" + mime), nil
}

// ValidateIcon refuses icon bytes that are not a PNG of 1 to 4096 pixels a side.
func ValidateIcon(data []byte) error {
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("invalid bundle icon: %w", err)
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > 4096 || config.Height > 4096 {
		return fmt.Errorf("bundle icon dimensions must be between 1 and 4096")
	}
	if _, err = png.Decode(bytes.NewReader(data)); err != nil {
		return fmt.Errorf("invalid bundle icon: %w", err)
	}
	return nil
}

// WriteReceipt writes r as dest's install receipt, owner-only.
func WriteReceipt(dest *os.Root, r Receipt) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return WriteFile(dest, ReceiptName, append(data, '\n'), 0o600)
}

// WriteFile creates name under root exclusively with data and mode, removing it again on a failed write.
func WriteFile(root *os.Root, name string, data []byte, mode fs.FileMode) (err error) {
	f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	err = errors.Join(err, f.Close())
	if err != nil {
		_ = root.Remove(name)
	}
	return err
}
