package sdk

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"time"
)

// Artifact describes one downloadable file in the dist directory.
type Artifact struct {
	// File is the dist-relative file name (one of GoArtifact, JSArtifact,
	// JSTypesArtifact).
	File string `json:"file"`
	// SHA256 is the lowercase hex digest of the file contents at
	// generation time. The serving side uses it as the ETag.
	SHA256 string `json:"sha256"`
	// Bytes is the file size, used for Content-Length and display.
	Bytes int64 `json:"bytes"`
	// Module is the Go module path baked into the Go SDK's go.mod.
	// Empty for non-Go artifacts.
	Module string `json:"module,omitempty"`
}

// Manifest is the machine-readable index `gofastr generate sdk` writes as
// manifest.json beside the artifacts. The serving side reads it to render
// download links (size, version, generated-at) and to detect drift between
// the generated artifacts and the live entity registry via SchemaHash.
type Manifest struct {
	SchemaVersion int `json:"schemaVersion"`
	// App is the display/app name the SDKs were generated for.
	App string `json:"app"`
	// SDKVersion is the semver stamped into the SDKs (0.0.0-dev default).
	SDKVersion string `json:"sdkVersion"`
	// GofastrVersion is the gofastr toolchain version that generated the
	// artifacts (from the host app's go.mod require line, or "dev").
	GofastrVersion string    `json:"gofastrVersion"`
	GeneratedAt    time.Time `json:"generatedAt"`
	// Entities lists the entity names the SDKs cover, sorted. The serving
	// side restricts its live SchemaHash computation to this set so the
	// drift check compares like with like even when the SDK was generated
	// with --only/--exclude.
	Entities []string `json:"entities"`
	// SchemaHash is SchemaHash() over the covered entities at generation
	// time ("sha256:<hex>").
	SchemaHash string `json:"schemaHash"`
	// Artifacts is keyed "go", "js", "js-types" — targets that were not
	// generated are absent.
	Artifacts map[string]Artifact `json:"artifacts"`
}

// ReadManifest loads and validates ManifestFile from the root of fsys
// (the dist directory). It returns an error when the file is missing,
// malformed, or structurally empty; a SchemaVersion mismatch is NOT an
// error — callers detect it via Manifest.SchemaVersion and degrade the
// drift check to "unknown provenance".
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
