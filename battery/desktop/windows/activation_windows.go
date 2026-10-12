//go:build windows && amd64

package windows

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows/registry"

	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/win32"
)

const (
	wmActivateExisting = win32.WM_APP + 75
	copyDataDeepLink   = 0x4746444C // "GFDL"
	maxProtocolURL     = 2048
	maxCopyDataBytes   = (maxProtocolURL + 1) * 2
)

type copyDataMessage struct {
	Tag   uintptr
	Bytes uint32
	Pad   uint32
	Data  uintptr
}

func (s *winShell) PrepareLaunch(appID, appName, scheme string) (bool, []string, error) {
	className := appWindowClassName(appID)
	s.mu.Lock()
	s.className = className
	s.deepLinkScheme = scheme
	s.mu.Unlock()
	if scheme == "" {
		return false, nil, nil
	}

	mutexName := appInstanceMutexName(appID)
	mutex, alreadyExists, err := win32.CreateNamedMutex(mutexName)
	if err != nil {
		return false, nil, fmt.Errorf("desktop/windows: reserve app instance: %w", err)
	}
	if alreadyExists {
		_ = win32.CloseHandle(mutex)
		return true, nil, s.forwardActivation(className, scheme)
	}

	s.mu.Lock()
	s.activationMutex = mutex
	s.mu.Unlock()

	if err := registerUserProtocol(scheme, appID, appName); err != nil {
		s.mu.Lock()
		s.activationMutex = 0
		s.mu.Unlock()
		_ = win32.CloseHandle(mutex)
		return false, nil, err
	}
	return false, protocolURLs(os.Args[1:], scheme), nil
}

func appWindowClassName(appID string) string {
	return windowClassName + "-" + stableIdentity(appID)
}

func appInstanceMutexName(appID string) string {
	return `Local\GoFastrDesktop-` + stableIdentity(appID)
}

func stableIdentity(appID string) string {
	// Keep names below Win32's named-object length limit without exposing
	// the reverse-DNS app id in the object namespace.
	sum := sha256.Sum256([]byte(appID))
	return fmt.Sprintf("%x", sum[:16])
}

func registerUserProtocol(scheme, appID, appName string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("desktop/windows: locate executable for URL registration: %w", err)
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return fmt.Errorf("desktop/windows: resolve executable for URL registration: %w", err)
	}
	command := fmt.Sprintf(`"%s" "%%1"`, exe)
	classPath := `Software\Classes\` + scheme
	if err := checkProtocolOwnership(classPath, appID, command); err != nil {
		return err
	}
	access := uint32(registry.SET_VALUE | registry.CREATE_SUB_KEY)
	key, _, err := registry.CreateKey(registry.CURRENT_USER, classPath, access)
	if err != nil {
		return fmt.Errorf("desktop/windows: register URL scheme %q for the current user: %w", scheme, err)
	}
	defer key.Close()
	for _, value := range []struct{ name, value string }{
		{"", "URL:" + appName + " Protocol"},
		{"URL Protocol", ""},
		{"GoFastrAppID", appID},
	} {
		if err := key.SetStringValue(value.name, value.value); err != nil {
			return fmt.Errorf("desktop/windows: write URL scheme %q registration: %w", scheme, err)
		}
	}
	commandKey, _, err := registry.CreateKey(registry.CURRENT_USER, classPath+`\shell\open\command`, access)
	if err != nil {
		return fmt.Errorf("desktop/windows: register URL scheme command: %w", err)
	}
	defer commandKey.Close()
	if err := commandKey.SetStringValue("", command); err != nil {
		return fmt.Errorf("desktop/windows: write URL scheme command: %w", err)
	}
	return nil
}

func checkProtocolOwnership(classPath, appID, command string) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, classPath, registry.QUERY_VALUE)
	if err == nil {
		defer key.Close()
		owner, _, ownerErr := key.GetStringValue("GoFastrAppID")
		if ownerErr == nil && owner != "" && owner != appID {
			return fmt.Errorf("desktop/windows: URL scheme %q is already registered by app %q", strings.TrimPrefix(classPath, `Software\Classes\`), owner)
		}
		if ownerErr != nil && !errors.Is(ownerErr, registry.ErrNotExist) {
			return fmt.Errorf("desktop/windows: read URL scheme owner: %w", ownerErr)
		}
		if owner == "" {
			current, currentErr := registry.OpenKey(registry.CURRENT_USER, classPath+`\shell\open\command`, registry.QUERY_VALUE)
			if currentErr != nil {
				if errors.Is(currentErr, registry.ErrNotExist) {
					return fmt.Errorf("desktop/windows: URL scheme %q is already registered by another per-user handler", strings.TrimPrefix(classPath, `Software\Classes\`))
				}
				return fmt.Errorf("desktop/windows: inspect URL scheme command: %w", currentErr)
			}
			registeredCommand, _, valueErr := current.GetStringValue("")
			_ = current.Close()
			if valueErr != nil {
				return fmt.Errorf("desktop/windows: read URL scheme command: %w", valueErr)
			}
			if registeredCommand != command {
				return fmt.Errorf("desktop/windows: URL scheme %q already has a different per-user handler", strings.TrimPrefix(classPath, `Software\Classes\`))
			}
		}
		return nil
	}
	if !errors.Is(err, registry.ErrNotExist) {
		return fmt.Errorf("desktop/windows: inspect URL scheme registration: %w", err)
	}
	if machineKey, machineErr := registry.OpenKey(registry.CLASSES_ROOT, strings.TrimPrefix(classPath, `Software\Classes\`), registry.QUERY_VALUE); machineErr == nil {
		_ = machineKey.Close()
		return fmt.Errorf("desktop/windows: URL scheme %q already has a machine-wide handler", strings.TrimPrefix(classPath, `Software\Classes\`))
	} else if !errors.Is(machineErr, registry.ErrNotExist) {
		return fmt.Errorf("desktop/windows: inspect machine URL scheme registration: %w", machineErr)
	}
	return nil
}

func protocolURLs(args []string, scheme string) []string {
	var urls []string
	for _, arg := range args {
		if len(arg) > maxProtocolURL {
			continue
		}
		u, err := url.Parse(arg)
		if err == nil && strings.EqualFold(u.Scheme, scheme) {
			urls = append(urls, arg)
		}
	}
	return urls
}

func (s *winShell) forwardActivation(className, scheme string) error {
	hwnd, err := waitForAppWindow(className)
	if err != nil {
		return err
	}
	for _, rawURL := range protocolURLs(os.Args[1:], scheme) {
		if err := sendDeepLinkMessage(hwnd, rawURL); err != nil {
			return fmt.Errorf("desktop/windows: forward URL activation: %w", err)
		}
	}
	if err := win32.PostMessage(hwnd, wmActivateExisting, 0, 0); err != nil {
		return fmt.Errorf("desktop/windows: focus the running app: %w", err)
	}
	return nil
}

func waitForAppWindow(className string) (uintptr, error) {
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if hwnd := win32.FindWindow(className, ""); hwnd != 0 {
			return hwnd, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return 0, fmt.Errorf("desktop/windows: running app did not create its window within 30 seconds")
}

func sendDeepLinkMessage(hwnd uintptr, rawURL string) error {
	wide, err := syscall.UTF16FromString(rawURL)
	if err != nil {
		return err
	}
	message := copyDataMessage{
		Tag:   copyDataDeepLink,
		Bytes: uint32(len(wide) * 2),
		Data:  uintptr(unsafe.Pointer(&wide[0])),
	}
	result, err := win32.SendMessageTimeout(hwnd, win32.WM_COPYDATA, 0,
		uintptr(unsafe.Pointer(&message)), 5_000)
	runtime.KeepAlive(wide)
	runtime.KeepAlive(message)
	if err != nil {
		return err
	}
	if result != 1 {
		return fmt.Errorf("running app refused the URL")
	}
	return nil
}

func (s *winShell) receiveDeepLink(hwnd, lparam uintptr) uintptr {
	if lparam == 0 {
		return 0
	}
	s.mu.RLock()
	mainHwnd := s.hwnd
	scheme := s.deepLinkScheme
	callback := s.onDeepLink
	s.mu.RUnlock()
	if hwnd != mainHwnd || scheme == "" || callback == nil {
		return 0
	}
	message := (*copyDataMessage)(unsafe.Pointer(lparam))
	if message.Tag != copyDataDeepLink || message.Data == 0 || message.Bytes < 2 ||
		message.Bytes > maxCopyDataBytes || message.Bytes%2 != 0 {
		return 0
	}
	units := unsafe.Slice((*uint16)(unsafe.Pointer(message.Data)), int(message.Bytes/2))
	if units[len(units)-1] != 0 {
		return 0
	}
	for _, unit := range units[:len(units)-1] {
		if unit == 0 {
			return 0
		}
	}
	rawURL := syscall.UTF16ToString(units)
	if len(rawURL) > maxProtocolURL {
		return 0
	}
	u, err := url.Parse(rawURL)
	if err != nil || !strings.EqualFold(u.Scheme, scheme) {
		return 0
	}
	go callback(rawURL)
	return 1
}

func (s *winShell) activateExistingWindow(hwnd uintptr) {
	win32.ShowWindowCmd(hwnd, win32.SW_RESTORE)
	win32.BringWindowToTop(hwnd)
	win32.SetForegroundWindow(hwnd)
}

func (s *winShell) releaseActivationMutex() {
	s.mu.Lock()
	mutex := s.activationMutex
	s.activationMutex = 0
	s.mu.Unlock()
	_ = win32.CloseHandle(mutex)
}
