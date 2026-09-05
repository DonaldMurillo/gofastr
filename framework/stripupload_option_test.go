package framework

import "testing"

// WithStripUploadMetadata reaches every CRUD handler the app builds, the
// way WithImagePipeline does.
func TestWithStripUploadMetadataReachesHandlers(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware(), WithStripUploadMetadata())
	if !app.stripUploadMetadata {
		t.Fatal("WithStripUploadMetadata did not set the app flag")
	}
	if NewApp(WithoutDefaultMiddleware()).stripUploadMetadata {
		t.Fatal("stripping must be off by default")
	}
}
