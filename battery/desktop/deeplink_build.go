package desktop

// builtDeepLinkScheme is set by `gofastr desktop build --scheme` for
// Windows executables. The desktop battery uses it to supply the default
// deep-link contract when the app did not declare one itself.
var builtDeepLinkScheme string
