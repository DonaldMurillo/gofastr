// Package sdk is the SDK generator's and docs site's shared manifest, schema hash and zip packer.
package sdk

import "strings"

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
	SchemaVersion = 3
)

// ExampleHost returns the public host for SDK examples, without trailing slashes.
func ExampleHost(baseURL string) string {
	if baseURL == "" {
		return "https://your-app.example.com"
	}
	return strings.TrimRight(baseURL, "/")
}

// File is one generated file: a dist-relative path and its bytes.
type File struct {
	Path string
	Data []byte
}
