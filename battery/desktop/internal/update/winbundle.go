package update

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/DonaldMurillo/gofastr/internal/winversion"
)

// WebView2LoaderName is the loader DLL a Windows desktop build ships
// beside its executable.
const WebView2LoaderName = "WebView2Loader.dll"

// WindowsBundleFiles names the files a Windows update archive's root
// directory holds, in swap order: the executable first.
func WindowsBundleFiles(name string) []string {
	return []string{name + ".exe", WebView2LoaderName}
}

// CheckWindowsBundle refuses an extracted Windows bundle root that
// holds anything other than exactly WindowsBundleFiles(name) as
// regular files. The set is closed: a stray file would land beside
// the executable with no swap or cleanup that knows about it.
func CheckWindowsBundle(dir, name string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("%w: bundle root unreadable", ErrBadArchive)
	}
	want := WindowsBundleFiles(name)
	if len(entries) != len(want) {
		return fmt.Errorf("%w: a Windows bundle holds exactly %s and %s", ErrBadArchive, want[0], want[1])
	}
	for _, e := range entries {
		if !slices.Contains(want, e.Name()) || !e.Type().IsRegular() {
			return fmt.Errorf("%w: a Windows bundle holds exactly %s and %s", ErrBadArchive, want[0], want[1])
		}
	}
	return nil
}

// ReadWindowsBundleInfo reads the identity the bundle's executable
// declares in its version resource: GoFastrAppID and ProductVersion,
// both of which `gofastr desktop build` writes.
func ReadWindowsBundleInfo(dir, name string) (BundleInfo, error) {
	return ReadExeInfo(filepath.Join(dir, name+".exe"))
}

// ReadExeInfo reads the identity a desktop build's executable declares.
func ReadExeInfo(exe string) (BundleInfo, error) {
	strs, err := winversion.ReadFile(exe)
	if err != nil {
		return BundleInfo{}, fmt.Errorf("%w: no version resource", ErrBadBundleInfo)
	}
	info := BundleInfo{Identifier: strs[winversion.KeyAppID], ShortVersion: strs[winversion.KeyProductVersion]}
	if info.Identifier == "" || info.ShortVersion == "" {
		return BundleInfo{}, fmt.Errorf("%w: the version resource has no %s or %s; rebuild with gofastr desktop build", ErrBadBundleInfo, winversion.KeyAppID, winversion.KeyProductVersion)
	}
	return info, nil
}
