package sdkdocs

import "github.com/DonaldMurillo/gofastr/core/i18n"

// The refusals this package writes, under their catalog keys.
var (
	reasonSDKArtifactMissing = i18n.Define("sdkdocs.artifact_missing", "SDK artifact missing: re-run `gofastr generate sdk`")
	reasonSDKNotGenerated    = i18n.Define("sdkdocs.not_generated", "SDK artifacts are not generated yet: run `gofastr generate sdk` and point sdkdocs.Config.Artifacts at gen/sdk/dist")
)
