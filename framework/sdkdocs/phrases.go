package sdkdocs

import "github.com/DonaldMurillo/gofastr/core/i18n"

var (
	reasonSDKArtifactMissing    = i18n.Define("sdkdocs.artifact_missing", "SDK artifact missing: re-run `gofastr generate sdk`")
	reasonSDKUndocumented       = i18n.Define("sdkdocs.undocumented", "SDK downloads are withheld: they cover entities this site does not document")
	reasonSDKUnpublishedOpenAPI = i18n.Define("sdkdocs.unpublished_openapi", "SDK downloads are withheld: they were generated from an OpenAPI document this site does not publish")
	reasonSDKNotGenerated       = i18n.Define("sdkdocs.not_generated", "SDK artifacts are not generated yet: run `gofastr generate sdk` and point sdkdocs.Config.Artifacts at gen/sdk/dist")
)
