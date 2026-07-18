// Package sdk is the shared contract between `gofastr generate sdk` (which
// emits client SDKs and packs the downloadable artifacts) and the serving
// side (framework/sdkdocs, which hosts them). It owns three things:
//
//   - the artifact manifest schema (manifest.json in the dist directory),
//   - the schema hash both sides compute to detect drift between the
//     artifacts a developer generated and the API the running app serves,
//   - the deterministic zip packer the generator uses so re-running
//     generation on an unchanged schema produces byte-identical archives.
//
// The package is a leaf: it imports only framework/entity and core/schema,
// so both cmd/gofastr and framework/uihost can depend on it without cycles.
package sdk

// Artifact and manifest file names inside the dist directory. The names are
// stable — regeneration overwrites in place — so download URLs never change;
// version and content hash travel in the manifest instead.
const (
	// ManifestFile is the machine-readable index of the dist directory.
	ManifestFile = "manifest.json"
	// GoArtifact is the downloadable Go SDK: a plain zip holding a
	// standalone stdlib-only module (client.go + go.mod + README.md).
	GoArtifact = "sdk-go.zip"
	// JSArtifact is the JS SDK: one handrolled ESM file, importable
	// directly from the served URL or dropped into a project. There is
	// deliberately no npm packaging — publishing is the app owner's call.
	JSArtifact = "client.js"
	// JSTypesArtifact is the TypeScript declaration file matching
	// JSArtifact.
	JSTypesArtifact = "client.d.ts"
	// SchemaVersion is the manifest schema version this package writes
	// and understands. A manifest with a different SchemaVersion is
	// treated as unknown provenance, not as stale (the hash algorithm may
	// have changed between gofastr versions).
	SchemaVersion = 1
)

// File is one generated file: a dist-relative path and its bytes. Data is
// []byte (not string) because artifacts include binary archives.
type File struct {
	Path string
	Data []byte
}
