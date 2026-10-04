//go:build windows

package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	webView2SDKVersion  = "1.0.4258.31"
	webView2NupkgSHA256 = "56f7f4b8bf9aee4b8efefbbdd4f67d5f74ebd1b100ed0806da71bf76af481aa9"
	webView2NupkgURL    = "https://api.nuget.org/v3-flatcontainer/microsoft.web.webview2/" + webView2SDKVersion + "/microsoft.web.webview2." + webView2SDKVersion + ".nupkg"
	webView2LoaderEntry = "runtimes/win-x64/native/WebView2Loader.dll"
)

func buildWindowsDesktop(f desktopBuildFlags, name string) error {
	if !validWindowsArtifactName(name) {
		return fmt.Errorf("--name %q cannot be used as a Windows executable name", name)
	}
	if _, err := windowsVersionParts(f.version); err != nil {
		return err
	}
	iconPNG, iconSource := loadIconSource(f.icon)
	icon, err := buildWindowsICO(iconPNG)
	if err != nil {
		return fmt.Errorf("build Windows icon from %s: %w", iconSource, err)
	}
	outDir := f.out
	if outDir == "" {
		outDir = "dist"
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("create output folder %s: %w", outDir, err)
	}

	loader, err := fetchWebView2Loader()
	if err != nil {
		return err
	}
	loaderTemp, err := writeArtifactTemp(outDir, ".WebView2Loader-*.dll", loader, 0o644)
	if err != nil {
		return fmt.Errorf("stage WebView2 loader: %w", err)
	}
	defer os.Remove(loaderTemp)

	exePath := filepath.Join(outDir, name+".exe")
	exeFile, err := os.CreateTemp(outDir, "."+name+"-*.exe")
	if err != nil {
		return fmt.Errorf("stage executable: %w", err)
	}
	exeTemp := exeFile.Name()
	if err := exeFile.Close(); err != nil {
		os.Remove(exeTemp)
		return err
	}
	if err := os.Remove(exeTemp); err != nil {
		return err
	}
	defer os.Remove(exeTemp)

	ldflags := windowsDesktopLinkerFlags(f.scheme)
	info("Building %s (windows/amd64, CGO_ENABLED=0)...", f.pkg)
	cmd := exec.Command("go", "build", "-trimpath", "-ldflags", ldflags, "-o", exeTemp, f.pkg)
	cmd.Env = desktopBuildEnvironment(map[string]string{"CGO_ENABLED": "0", "GOOS": "windows", "GOARCH": "amd64"})
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go build: %w", err)
	}
	if st, err := os.Stat(exeTemp); err != nil || st.IsDir() {
		return fmt.Errorf("go build did not produce %s", exeTemp)
	}
	if err := writeWindowsVersionResource(exeTemp, windowsDesktopVersion{
		Name:    name,
		ID:      f.id,
		Version: f.version,
	}); err != nil {
		return fmt.Errorf("embed Windows version resource: %w", err)
	}
	if err := writeWindowsIconResource(exeTemp, icon); err != nil {
		return fmt.Errorf("embed Windows application icon: %w", err)
	}

	loaderPath := filepath.Join(outDir, "WebView2Loader.dll")
	if err := replaceArtifact(loaderTemp, loaderPath); err != nil {
		return fmt.Errorf("install WebView2Loader.dll: %w", err)
	}
	if err := replaceArtifact(exeTemp, exePath); err != nil {
		return fmt.Errorf("install %s: %w", exePath, err)
	}
	success("Wrote %s with version information and an application icon, plus WebView2Loader.dll", exePath)
	info("Icon source: %s.", iconSource)
	return nil
}

func windowsDesktopLinkerFlags(scheme string) string {
	flags := "-s -w -H windowsgui"
	if scheme != "" {
		flags += " -X github.com/DonaldMurillo/gofastr/battery/desktop.builtDeepLinkScheme=" + scheme
	}
	return flags
}

func fetchWebView2Loader() ([]byte, error) {
	cacheBase, err := os.UserCacheDir()
	if err != nil {
		cacheBase = os.TempDir()
	}
	cacheDir := filepath.Join(cacheBase, "gofastr", "webview2", webView2SDKVersion)
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return nil, fmt.Errorf("create WebView2 cache: %w", err)
	}
	packagePath := filepath.Join(cacheDir, "microsoft.web.webview2.nupkg")
	data, err := os.ReadFile(packagePath)
	if err != nil || fmt.Sprintf("%x", sha256.Sum256(data)) != webView2NupkgSHA256 {
		if err := os.Remove(packagePath); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, webView2NupkgURL, nil)
		if err != nil {
			return nil, err
		}
		resp, err := (&http.Client{Timeout: 2 * time.Minute}).Do(req)
		if err != nil {
			return nil, fmt.Errorf("download Microsoft.Web.WebView2 %s: %w", webView2SDKVersion, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("download Microsoft.Web.WebView2: HTTP %s", resp.Status)
		}
		data, err = io.ReadAll(io.LimitReader(resp.Body, 20<<20))
		if err != nil {
			return nil, fmt.Errorf("read WebView2 SDK package: %w", err)
		}
		if got := fmt.Sprintf("%x", sha256.Sum256(data)); got != webView2NupkgSHA256 {
			return nil, fmt.Errorf("Microsoft.Web.WebView2 package checksum mismatch: got %s", got)
		}
		if err := os.WriteFile(packagePath, data, 0o600); err != nil {
			return nil, fmt.Errorf("cache WebView2 SDK package: %w", err)
		}
	}

	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open WebView2 SDK package: %w", err)
	}
	for _, entry := range archive.File {
		if entry.Name != webView2LoaderEntry {
			continue
		}
		if entry.UncompressedSize64 == 0 || entry.UncompressedSize64 > 8<<20 {
			return nil, fmt.Errorf("WebView2 loader has an unexpected size")
		}
		r, err := entry.Open()
		if err != nil {
			return nil, fmt.Errorf("open WebView2 loader in package: %w", err)
		}
		loader, readErr := io.ReadAll(io.LimitReader(r, 8<<20))
		closeErr := r.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if len(loader) == 0 {
			return nil, fmt.Errorf("WebView2 loader is empty")
		}
		return loader, nil
	}
	return nil, fmt.Errorf("Microsoft.Web.WebView2 %s package has no x64 loader at %s", webView2SDKVersion, webView2LoaderEntry)
}

func writeArtifactTemp(dir, pattern string, data []byte, mode os.FileMode) (string, error) {
	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", err
	}
	path := f.Name()
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(path)
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return "", err
	}
	if err := os.Chmod(path, mode); err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
}

func replaceArtifact(from, to string) error {
	if err := os.Remove(to); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(from, to)
}

func desktopBuildEnvironment(values map[string]string) []string {
	env := os.Environ()
	for key, value := range values {
		prefix := key + "="
		found := false
		for i, item := range env {
			if strings.HasPrefix(strings.ToUpper(item), strings.ToUpper(prefix)) {
				env[i] = prefix + value
				found = true
			}
		}
		if !found {
			env = append(env, prefix+value)
		}
	}
	return env
}
