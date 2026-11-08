// Package releasecmd implements `gofastr release`: cross-compiling a
// CLI or service binary for every supported OS and CPU, packing each
// build into an archive, and writing checksums.txt plus the install.sh
// and install.ps1 scripts that download, verify and install the build
// for the machine they run on.
package releasecmd
