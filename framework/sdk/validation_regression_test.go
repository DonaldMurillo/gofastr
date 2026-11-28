package sdk

import (
	"testing"
	"testing/fstest"
)

func TestPackZipAllowsDotsInFileNames(t *testing.T) {
	if _, err := PackZip("release..candidate", []File{{Path: "examples/client..test.js"}}); err != nil {
		t.Fatal(err)
	}
}

func TestPackZipRejectsDuplicateNames(t *testing.T) {
	if _, err := PackZip("sdk", []File{{Path: "client.js", Data: []byte("a")}, {Path: "client.js", Data: []byte("b")}}); err == nil {
		t.Fatal("accepted two entries with the same name")
	}
}

func TestReadManifestRejectsNegativeArtifactSize(t *testing.T) {
	body := []byte(`{"schemaVersion":2,"app":"x","entities":["item"],"schemaHash":"sha256:ab","artifacts":{"go":{"file":"sdk-go.zip","sha256":"ab","bytes":-1,"directory":"x-sdk"}}}`)
	if _, err := ReadManifest(fstest.MapFS{ManifestFile: {Data: body}}); err == nil {
		t.Fatal("accepted an artifact with negative size")
	}
}

func TestPackZipRejectsFileDirectoryConflict(t *testing.T) {
	if _, err := PackZip("sdk", []File{{Path: "a"}, {Path: "a-b"}, {Path: "a/b"}}); err == nil {
		t.Fatal("pack accepted a file also needed as a directory")
	}
}
