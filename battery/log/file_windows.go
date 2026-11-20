//go:build windows

package log

import "os"

const unixNoFollow = 0

func ownerUID(os.FileInfo) (int, bool) { return 0, false }
