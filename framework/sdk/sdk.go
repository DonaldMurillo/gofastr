// Package sdk is the SDK generator's and docs site's shared manifest, schema hash and zip packer.
package sdk

const (
	// ManifestFile is the machine-readable index of the dist directory.
	ManifestFile = "manifest.json"
	// GoArtifact is the Go SDK, a zip of a stdlib-only module.
	GoArtifact = "sdk-go.zip"
	// JSArtifact is the JS SDK, one ESM file.
	JSArtifact = "client.js"
	// JSTypesArtifact is the TypeScript declaration file for JSArtifact.
	JSTypesArtifact = "client.d.ts"
	// SchemaVersion is the manifest version this package writes.
	SchemaVersion = 2
)

// File is one generated file: a dist-relative path and its bytes.
type File struct {
	Path string
	Data []byte
}
