package gallery

import "testing"

func TestImageDemosLinkUIPackage(t *testing.T) {
	for _, slug := range []string{"image", "pipelineimage"} {
		if got := PkgForSlug(slug); got != "framework/ui" {
			t.Errorf("%s API package = %s, want framework/ui", slug, got)
		}
	}
}
