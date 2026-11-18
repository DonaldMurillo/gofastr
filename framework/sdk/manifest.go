package sdk

import (
	"encoding/json"
	"fmt"
	"io/fs"
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

// ReadManifest loads ManifestFile from fsys, erroring when it is missing,
// malformed, or lacks a version, entities, artifacts or a file or sha256.
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
	}
	return &m, nil
}
