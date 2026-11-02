// Package winversion writes and reads the VERSIONINFO resource a
// Windows desktop build carries.
//
// `gofastr desktop build` renders the resource with Render and embeds
// it in the executable. The auto-updater reads it back with ReadFile,
// from the running executable and from a downloaded one, and
// `gofastr desktop feed` reads it from the archive it signs. All three
// run through this package so the writer and the readers agree on the
// bytes. The reader is pure Go, so the feed writer works
// on any host, not only Windows.
package winversion
