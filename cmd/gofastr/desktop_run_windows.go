//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func prepareDesktopRun(binary string) error {
	loader, err := fetchWebView2Loader()
	if err != nil {
		return err
	}
	dir := filepath.Dir(binary)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create private desktop run folder: %w", err)
	}
	staged, err := writeArtifactTemp(dir, ".WebView2Loader-*.dll", loader, 0o600)
	if err != nil {
		return fmt.Errorf("stage WebView2 loader: %w", err)
	}
	defer os.Remove(staged)
	if err := replaceArtifact(staged, filepath.Join(dir, "WebView2Loader.dll")); err != nil {
		return fmt.Errorf("install WebView2Loader.dll: %w", err)
	}
	return nil
}
