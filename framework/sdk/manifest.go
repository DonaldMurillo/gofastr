package sdk

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"regexp"
	"time"
)

// Artifact describes one downloadable file in the dist directory.
type Artifact struct {
	// File is the dist-relative file name.
	File string `json:"file"`
	// SHA256 is the lowercase hex digest of the file, served as its ETag.
	SHA256 string `json:"sha256"`
	// Bytes is the file size.
	Bytes int64 `json:"bytes"`
	// Module is the Go SDK's module path, empty for other artifacts.
	Module string `json:"module,omitempty"`
	// Directory is the Go archive's top-level directory, empty for other artifacts.
	Directory string `json:"directory,omitempty"`
}

// Manifest is the manifest.json written beside the artifacts.
type Manifest struct {
	SchemaVersion int `json:"schemaVersion"`
	// App is the app name the SDKs were generated for.
	App string `json:"app"`
	// SDKVersion is the semver stamped into the SDKs.
	SDKVersion string `json:"sdkVersion"`
	// GofastrVersion is the gofastr version that generated them, or "dev".
	GofastrVersion string `json:"gofastrVersion"`
	// GeneratedAt is set only by --stamp-time; zero means unstamped.
	GeneratedAt time.Time `json:"generatedAt,omitzero"`
	// Entities lists, sorted, the entities the SDKs and the hash cover.
	Entities []string `json:"entities"`
	// SchemaHash is SchemaHash over Entities at generation time.
	SchemaHash string `json:"schemaHash"`
	// Artifacts is keyed "go", "js" and "js-types", for those generated.
	Artifacts map[string]Artifact `json:"artifacts"`
}

var archiveDirectory = regexp.MustCompile(`^[A-Za-z0-9._~-]+$`)

// ReadManifest loads ManifestFile from fsys and rejects incomplete artifact
// records or a Go archive without a safe top-level directory.
func ReadManifest(fsys fs.FS) (*Manifest, error) {
	raw, err := fs.ReadFile(fsys, ManifestFile)
	if err != nil {
		return nil, fmt.Errorf("sdk: read %s: %w", ManifestFile, err)
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("sdk: parse %s: %w", ManifestFile, err)
	}
	if m.SchemaVersion == 0 {
		return nil, fmt.Errorf("sdk: %s: missing schemaVersion", ManifestFile)
	}

	if len(m.Entities) == 0 {
		return nil, fmt.Errorf("sdk: %s: no entities listed", ManifestFile)
	}
	if len(m.Artifacts) == 0 {
		return nil, fmt.Errorf("sdk: %s: no artifacts listed", ManifestFile)
	}
	for key, a := range m.Artifacts {
		if a.File == "" || a.SHA256 == "" {
			return nil, fmt.Errorf("sdk: %s: artifact %q is missing file or sha256", ManifestFile, key)
		}
		if key == "go" && (!archiveDirectory.MatchString(a.Directory) || a.Directory == "." || a.Directory == "..") {
			return nil, fmt.Errorf("sdk: %s: Go artifact directory must be a safe single path component", ManifestFile)
		}
	}
	return &m, nil
}
