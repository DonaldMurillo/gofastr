package packcmd

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	_ "github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/packcmd/testdata/pluginreceivers" // Compile the same fixture pack reads.
)

func TestPackReadStubsRecoversPointerAndGenericPlugins(t *testing.T) {
	bp := &appspec.AppSpec{}
	if err := packReadStubs(filepath.Join("testdata", "pluginreceivers"), bp); err != nil {
		t.Fatalf("packReadStubs: %v", err)
	}
	var names []string
	for _, plugin := range bp.Plugins {
		names = append(names, plugin.Name)
	}
	if want := []string{"admin", "report", "export"}; !slices.Equal(names, want) {
		t.Fatalf("packed plugins = %v, want %v", names, want)
	}
}
