//go:build windows

package evalrunner

import "path/filepath"

func executableName(name string) string {
	if filepath.Ext(name) == "" {
		return name + ".exe"
	}
	return name
}
