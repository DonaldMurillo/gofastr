//go:build windows && amd64

// Package win32 contains the small Win32 ABI layer used by the Windows
// desktop host. It deliberately exposes handles and procedure addresses,
// leaving policy in battery/desktop/windows.
package win32

import (
	"encoding/binary"
	"fmt"
	"syscall"
	"unsafe"
)

const (
	WS_OVERLAPPEDWINDOW = 0x00CF0000
	WS_THICKFRAME       = 0x00040000
	WS_MAXIMIZEBOX      = 0x00010000
	WS_VISIBLE          = 0x10000000
	WS_CHILD            = 0x40000000
	WS_CLIPCHILDREN     = 0x02000000
	WS_CLIPSIBLINGS     = 0x04000000
	WS_EX_NOACTIVATE    = 0x08000000
	CW_USEDEFAULT       = 0x80000000
	WM_DESTROY          = 0x0002
	WM_SIZE             = 0x0005
	WM_COMMAND          = 0x0111
	WM_CLOSE            = 0x0010
	WM_APP              = 0x8000
	PM_REMOVE           = 0x0001
	SW_SHOW             = 5
	SW_RESTORE          = 9
	SW_SHOWNOACTIVATE   = 4
	MA_NOACTIVATE       = 3
	GWLP_USERDATA       = -21
	GWL_STYLE           = -16
	SWP_NOSIZE          = 0x0001
	SWP_NOMOVE          = 0x0002
	SWP_NOZORDER        = 0x0004
	SWP_NOACTIVATE      = 0x0010
	SWP_FRAMECHANGED    = 0x0020
	HTClient            = 1
	HTCaption           = 2
	HTMenu              = 5
	HTLeft              = 10
	HTRight             = 11
	HTTop               = 12
	HTTopLeft           = 13
	HTTopRight          = 14
	HTBottom            = 15
	HTBottomLeft        = 16
	HTBottomRight       = 17
	MF_STRING           = 0x0000
	MF_POPUP            = 0x0010
	MF_SEPARATOR        = 0x0800
	MF_CHECKED          = 0x0008
	MF_GRAYED           = 0x0001
	TPM_RETURNCMD       = 0x0100
	TPM_RIGHTBUTTON     = 0x0002
)

const (
	user32Name   = "user32.dll"
	kernel32Name = "kernel32.dll"
)

var (
	user32   = syscall.NewLazyDLL(user32Name)
	kernel32 = syscall.NewLazyDLL(kernel32Name)
)

func call(dll *syscall.LazyDLL, name string, args ...uintptr) (uintptr, uintptr, error) {
	p := dll.NewProc(name)
	if err := p.Find(); err != nil {
		return 0, 0, err
	}
	r1, r2, e := syscall.SyscallN(p.Addr(), args...)
	if e != 0 {
		return r1, r2, e
	}
	return r1, r2, nil
}

func UTF16(s string) (*uint16, error) { return syscall.UTF16PtrFromString(s) }

func ModuleHandle() uintptr {
	r, _, _ := call(kernel32, "GetModuleHandleW", 0)
	return r
}

func CurrentThreadID() uint32 {
	r, _, _ := call(kernel32, "GetCurrentThreadId")
	return uint32(r)
}

func PostMessage(hwnd uintptr, msg uint32, wp, lp uintptr) error {
	r, _, err := call(user32, "PostMessageW", hwnd, uintptr(msg), wp, lp)
	if r == 0 {
		return fmt.Errorf("PostMessageW: %w", lastError(err))
	}
	return nil
}

func PostThreadMessage(threadID uint32, msg uint32, wp, lp uintptr) error {
	r, _, err := call(user32, "PostThreadMessageW", uintptr(threadID), uintptr(msg), wp, lp)
	if r == 0 {
		return fmt.Errorf("PostThreadMessageW: %w", lastError(err))
	}
	return nil
}

func lastError(err error) error {
	if err != nil {
		return err
	}
	return syscall.GetLastError()
}

type WindowClass struct {
	Style      uint32
	WndProc    uintptr
	ClassName  string
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	Instance   uintptr
}

type wndClassExW struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSmall  uintptr
}

func RegisterWindowClass(c WindowClass) error {
	name, err := UTF16(c.ClassName)
	if err != nil {
		return err
	}
	instance := c.Instance
	if instance == 0 {
		instance = ModuleHandle()
	}
	wc := wndClassExW{
		Size:       uint32(unsafe.Sizeof(wndClassExW{})),
		Style:      c.Style,
		WndProc:    c.WndProc,
		Instance:   instance,
		Icon:       c.Icon,
		Cursor:     c.Cursor,
		Background: c.Background,
		ClassName:  name,
	}
	r, _, callErr := call(user32, "RegisterClassExW", uintptr(unsafe.Pointer(&wc)))
	if r == 0 {
		// ERROR_CLASS_ALREADY_EXISTS is a normal second shell construction.
		if errno, ok := lastError(callErr).(syscall.Errno); ok && errno == 1410 {
			return nil
		}
		return fmt.Errorf("RegisterClassExW(%q): %w", c.ClassName, lastError(callErr))
	}
	return nil
}

func CreateWindow(className, title string, style uint32, x, y, width, height int32, parent uintptr) (uintptr, error) {
	return CreateWindowEx(0, className, title, style, x, y, width, height, parent)
}

func CreateWindowEx(exStyle uint32, className, title string, style uint32, x, y, width, height int32, parent uintptr) (uintptr, error) {
	class, err := UTF16(className)
	if err != nil {
		return 0, err
	}
	caption, err := UTF16(title)
	if err != nil {
		return 0, err
	}
	h, _, callErr := call(user32, "CreateWindowExW", uintptr(exStyle),
		uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(caption)), uintptr(style),
		uintptr(uint32(x)), uintptr(uint32(y)), uintptr(uint32(width)), uintptr(uint32(height)),
		parent, 0, ModuleHandle(), 0)
	if h == 0 {
		return 0, fmt.Errorf("CreateWindowExW: %w", lastError(callErr))
	}
	return h, nil
}

func ShowWindow(hwnd uintptr)             { _, _, _ = call(user32, "ShowWindow", hwnd, SW_SHOW) }
func ShowWindowCmd(hwnd uintptr, cmd int) { _, _, _ = call(user32, "ShowWindow", hwnd, uintptr(cmd)) }
func UpdateWindow(hwnd uintptr)           { _, _, _ = call(user32, "UpdateWindow", hwnd) }
func DestroyWindow(hwnd uintptr)          { _, _, _ = call(user32, "DestroyWindow", hwnd) }
func DefWindowProc(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
	r, _, _ := call(user32, "DefWindowProcW", hwnd, uintptr(msg), wp, lp)
	return r
}
func SendMessage(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
	r, _, _ := call(user32, "SendMessageW", hwnd, uintptr(msg), wp, lp)
	return r
}

type keyboardInput struct {
	Type uint32
	_    uint32
	Data [32]byte // INPUT's union is 32 bytes on amd64.
}

// SendKeyboardShortcut emits trusted keyboard input to the foreground window.
// Native menu commands use this to follow Chromium's normal edit/clipboard path.
func SendKeyboardShortcut(key uint16, modifiers ...uint16) error {
	if key == 0 || len(modifiers) > 3 {
		return fmt.Errorf("SendInput: invalid keyboard shortcut")
	}
	var inputs [8]keyboardInput
	count := 0
	add := func(vk uint16, flags uint32) {
		input := &inputs[count]
		input.Type = 1 // INPUT_KEYBOARD
		binary.LittleEndian.PutUint16(input.Data[0:2], vk)
		binary.LittleEndian.PutUint32(input.Data[4:8], flags)
		count++
	}
	for _, modifier := range modifiers {
		add(modifier, 0)
	}
	add(key, 0)
	add(key, 0x0002 /* KEYEVENTF_KEYUP */)
	for i := len(modifiers) - 1; i >= 0; i-- {
		add(modifiers[i], 0x0002 /* KEYEVENTF_KEYUP */)
	}
	r, _, callErr := call(user32, "SendInput", uintptr(count), uintptr(unsafe.Pointer(&inputs[0])), unsafe.Sizeof(inputs[0]))
	if r != uintptr(count) {
		if callErr != 0 {
			return fmt.Errorf("SendInput: %w", lastError(callErr))
		}
		return fmt.Errorf("SendInput inserted %d of %d keyboard events", r, count)
	}
	return nil
}
func ReleaseCapture() { _, _, _ = call(user32, "ReleaseCapture") }
func SetWindowText(hwnd uintptr, title string) error {
	p, err := UTF16(title)
	if err != nil {
		return err
	}
	r, _, callErr := call(user32, "SetWindowTextW", hwnd, uintptr(unsafe.Pointer(p)))
	if r == 0 {
		return fmt.Errorf("SetWindowTextW: %w", lastError(callErr))
	}
	return nil
}

func GetWindowText(hwnd uintptr) string {
	buf := make([]uint16, 1024)
	n, _, _ := call(user32, "GetWindowTextW", hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf[:n])
}

func GetClientSize(hwnd uintptr) (int, int) {
	var rect struct{ Left, Top, Right, Bottom int32 }
	_, _, _ = call(user32, "GetClientRect", hwnd, uintptr(unsafe.Pointer(&rect)))
	return int(rect.Right - rect.Left), int(rect.Bottom - rect.Top)
}

func MoveWindow(hwnd uintptr, x, y, width, height int, repaint bool) error {
	paint := uintptr(0)
	if repaint {
		paint = 1
	}
	r, _, callErr := call(user32, "MoveWindow", hwnd, uintptr(x), uintptr(y), uintptr(width), uintptr(height), paint)
	if r == 0 {
		return fmt.Errorf("MoveWindow: %w", lastError(callErr))
	}
	return nil
}

type Message struct {
	Hwnd    uintptr
	Message uint32
	Padding uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pad2    uint32
	PtX     int32
	PtY     int32
	Private uint32
}

// GetMessage returns (false, nil) after WM_QUIT and (false, err) on failure.
func GetMessage(m *Message) (bool, error) {
	r, _, callErr := call(user32, "GetMessageW", uintptr(unsafe.Pointer(m)), 0, 0, 0)
	if int32(r) == -1 {
		return false, fmt.Errorf("GetMessageW: %w", lastError(callErr))
	}
	return r != 0, nil
}

func TranslateDispatch(m *Message) {
	ptr := uintptr(unsafe.Pointer(m))
	_, _, _ = call(user32, "TranslateMessage", ptr)
	_, _, _ = call(user32, "DispatchMessageW", ptr)
}

func PostQuitMessage(code int)         { _, _, _ = call(user32, "PostQuitMessage", uintptr(code)) }
func SetForegroundWindow(hwnd uintptr) { _, _, _ = call(user32, "SetForegroundWindow", hwnd) }
func BringWindowToTop(hwnd uintptr)    { _, _, _ = call(user32, "BringWindowToTop", hwnd) }

func CreateMenu() (uintptr, error) {
	r, _, err := call(user32, "CreateMenu")
	if r == 0 {
		return 0, fmt.Errorf("CreateMenu: %w", lastError(err))
	}
	return r, nil
}
func CreatePopupMenu() (uintptr, error) {
	r, _, err := call(user32, "CreatePopupMenu")
	if r == 0 {
		return 0, fmt.Errorf("CreatePopupMenu: %w", lastError(err))
	}
	return r, nil
}
func AppendMenu(menu uintptr, flags uint32, idOrSubmenu uintptr, title string) error {
	var p uintptr
	if title != "" {
		w, err := UTF16(title)
		if err != nil {
			return err
		}
		p = uintptr(unsafe.Pointer(w))
	}
	r, _, callErr := call(user32, "AppendMenuW", menu, uintptr(flags), idOrSubmenu, p)
	if r == 0 {
		return fmt.Errorf("AppendMenuW: %w", lastError(callErr))
	}
	return nil
}
func SetMenu(hwnd, menu uintptr) error {
	r, _, err := call(user32, "SetMenu", hwnd, menu)
	if r == 0 {
		return fmt.Errorf("SetMenu: %w", lastError(err))
	}
	return nil
}
func DrawMenuBar(hwnd uintptr) { _, _, _ = call(user32, "DrawMenuBar", hwnd) }

func TrackPopupMenu(menu, hwnd uintptr, x, y int) uintptr {
	r, _, _ := call(user32, "TrackPopupMenu", menu, TPM_RETURNCMD|TPM_RIGHTBUTTON,
		uintptr(x), uintptr(y), 0, hwnd, 0)
	return r
}

func MessageBox(hwnd uintptr, text, caption string, flags uint32) int32 {
	pText, err := UTF16(text)
	if err != nil {
		return 0
	}
	pCaption, err := UTF16(caption)
	if err != nil {
		return 0
	}
	r, _, _ := call(user32, "MessageBoxW", hwnd, uintptr(unsafe.Pointer(pText)), uintptr(unsafe.Pointer(pCaption)), uintptr(flags))
	return int32(r)
}

func LoadCursor(id uintptr) uintptr {
	r, _, _ := call(user32, "LoadCursorW", 0, id)
	return r
}

func ClientToScreen(hwnd uintptr, x, y int32) (int32, int32) {
	p := struct{ X, Y int32 }{x, y}
	_, _, _ = call(user32, "ClientToScreen", hwnd, uintptr(unsafe.Pointer(&p)))
	return p.X, p.Y
}

func SetWindowLongPtr(hwnd uintptr, index int32, value uintptr) uintptr {
	r, _, _ := call(user32, "SetWindowLongPtrW", hwnd, uintptr(uint32(index)), value)
	return r
}

func GetWindowLongPtr(hwnd uintptr, index int32) uintptr {
	r, _, _ := call(user32, "GetWindowLongPtrW", hwnd, uintptr(uint32(index)))
	return r
}

func FrameChanged(hwnd uintptr) {
	_, _, _ = call(user32, "SetWindowPos", hwnd, 0, 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE|SWP_NOZORDER|SWP_FRAMECHANGED)
}

func SetWindowPos(hwnd uintptr, x, y, width, height int32, flags uint32) error {
	r, _, callErr := call(user32, "SetWindowPos", hwnd, 0,
		uintptr(uint32(x)), uintptr(uint32(y)), uintptr(uint32(width)), uintptr(uint32(height)), uintptr(flags))
	if r == 0 {
		return fmt.Errorf("SetWindowPos: %w", lastError(callErr))
	}
	return nil
}

func Menu(hwnd uintptr) uintptr {
	r, _, _ := call(user32, "GetMenu", hwnd)
	return r
}

func WindowDPI(hwnd uintptr) uint32 {
	r, _, err := call(user32, "GetDpiForWindow", hwnd)
	if err != nil || r == 0 {
		return SystemDPI()
	}
	return uint32(r)
}

func SystemDPI() uint32 {
	r, _, err := call(user32, "GetDpiForSystem")
	if err != nil || r == 0 {
		return 96
	}
	return uint32(r)
}

func SystemMetricForDPI(index int32, dpi uint32) int32 {
	if dpi == 0 {
		dpi = 96
	}
	r, _, err := call(user32, "GetSystemMetricsForDpi", uintptr(uint32(index)), uintptr(dpi))
	if err != nil {
		return SystemMetric(index)
	}
	return int32(r)
}

// EnablePerMonitorV2DPI opts the process into native high-DPI rendering
// before it creates a window. If a host already selected per-monitor DPI
// awareness, it is already safe to create crisp windows.
func EnablePerMonitorV2DPI() bool {
	const perMonitorV2 = ^uintptr(3) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 (-4)
	r, _, _ := call(user32, "SetProcessDpiAwarenessContext", perMonitorV2)
	if r != 0 {
		return true
	}
	context, _, err := call(user32, "GetThreadDpiAwarenessContext")
	if err != nil || context == 0 {
		return false
	}
	awareness, _, err := call(user32, "GetAwarenessFromDpiAwarenessContext", context)
	return err == nil && int32(awareness) == 2 // PROCESS_PER_MONITOR_DPI_AWARE
}

func WindowFromPoint(x, y int32) uintptr {
	r, _, _ := call(user32, "WindowFromPoint", uintptr(uint32(y))<<32|uintptr(uint32(x)))
	return r
}

func IsWindowVisible(hwnd uintptr) bool {
	r, _, _ := call(user32, "IsWindowVisible", hwnd)
	return r != 0
}

func IsZoomed(hwnd uintptr) bool {
	r, _, _ := call(user32, "IsZoomed", hwnd)
	return r != 0
}

func IsIconic(hwnd uintptr) bool {
	r, _, _ := call(user32, "IsIconic", hwnd)
	return r != 0
}

func SystemMetric(index int32) int32 {
	r, _, _ := call(user32, "GetSystemMetrics", uintptr(uint32(index)))
	return int32(r)
}

func GetWindowRect(hwnd uintptr) (left, top, right, bottom int32, ok bool) {
	var r struct{ Left, Top, Right, Bottom int32 }
	v, _, _ := call(user32, "GetWindowRect", hwnd, uintptr(unsafe.Pointer(&r)))
	return r.Left, r.Top, r.Right, r.Bottom, v != 0
}

func GetForegroundWindow() uintptr {
	r, _, _ := call(user32, "GetForegroundWindow")
	return r
}

func KeyDown(vkey uint16) bool {
	r, _, _ := call(user32, "GetKeyState", uintptr(vkey))
	return int16(r) < 0
}

func SetTopMost(hwnd uintptr) {
	const (
		hwndTopMost   = ^uintptr(0)
		swpNoSize     = 0x0001
		swpNoMove     = 0x0002
		swpNoActivate = 0x0010
	)
	_, _, _ = call(user32, "SetWindowPos", hwnd, hwndTopMost, 0, 0, 0, 0, swpNoSize|swpNoMove|swpNoActivate)
}

func FindWindow(className, title string) uintptr {
	var class, caption *uint16
	if className != "" {
		class, _ = UTF16(className)
	}
	if title != "" {
		caption, _ = UTF16(title)
	}
	r, _, _ := call(user32, "FindWindowW", uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(caption)))
	return r
}

func FindWindowEx(parent uintptr, className string, title *uint16) uintptr {
	class, _ := UTF16(className)
	r, _, _ := call(user32, "FindWindowExW", parent, 0, uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(title)))
	return r
}

func ClickWindow(hwnd uintptr) {
	_, _, _ = call(user32, "SendMessageW", hwnd, 0x00F5 /* BM_CLICK */, 0, 0)
}

func CursorPosition() (int32, int32) {
	var p struct{ X, Y int32 }
	_, _, _ = call(user32, "GetCursorPos", uintptr(unsafe.Pointer(&p)))
	return p.X, p.Y
}

func IsWindow(hwnd uintptr) bool {
	r, _, _ := call(user32, "IsWindow", hwnd)
	return r != 0
}

func DwmSetWindowAttribute(hwnd uintptr, attribute uint32, value uint32) error {
	dwm := syscall.NewLazyDLL("dwmapi.dll")
	proc := dwm.NewProc("DwmSetWindowAttribute")
	if err := proc.Find(); err != nil {
		return err
	}
	r, _, _ := syscall.SyscallN(proc.Addr(), hwnd, uintptr(attribute), uintptr(unsafe.Pointer(&value)), unsafe.Sizeof(value))
	if int32(r) < 0 {
		return fmt.Errorf("DwmSetWindowAttribute: HRESULT 0x%08x", uint32(r))
	}
	return nil
}

func DwmDefWindowProc(hwnd uintptr, msg uint32, wp, lp uintptr) (uintptr, bool) {
	dwm := syscall.NewLazyDLL("dwmapi.dll")
	proc := dwm.NewProc("DwmDefWindowProc")
	if err := proc.Find(); err != nil {
		return 0, false
	}
	var result uintptr
	r, _, _ := syscall.SyscallN(proc.Addr(), hwnd, uintptr(msg), wp, lp, uintptr(unsafe.Pointer(&result)))
	return result, r != 0
}

func CurrentProcessID() uint32 {
	r, _, _ := call(kernel32, "GetCurrentProcessId")
	return uint32(r)
}

func MakeIntResource(i uint16) uintptr { return uintptr(i) }
