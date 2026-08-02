//go:build !windows

package fileperm

func RestrictDirectoryTree(_ string, _ string) error { return nil }
