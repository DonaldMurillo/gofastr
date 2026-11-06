package packcmd

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
)

func TestPackPreservesAppMetadata(t *testing.T) {
	spec := appspec.AppSpec{App: appspec.AppSpecApp{
		Name: "Metadata", Module: "example.com/metadata", DBDriver: "sqlite", DBURL: "file:metadata.db",
		Description: "An authored application description.", BaseURL: "https://metadata.example", PublicOpenAPI: true,
	}}
	packed, err := packAppSpec(materializeAppSpec(t, spec))
	if err != nil {
		t.Fatal(err)
	}
	if packed.App.Description != spec.App.Description || packed.App.BaseURL != spec.App.BaseURL || !packed.App.PublicOpenAPI {
		t.Fatalf("app metadata lost: description=%q base_url=%q public_openapi=%v", packed.App.Description, packed.App.BaseURL, packed.App.PublicOpenAPI)
	}
}
