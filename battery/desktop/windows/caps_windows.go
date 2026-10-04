//go:build windows && amd64

package windows

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/win32"
)

const (
	cfUnicodeText       = 13
	gmemMoveable        = 0x0002
	gmemZeroInit        = 0x0040
	fileOpenDialogCLSID = "DC1C5A9C-E88A-4DDE-A5A1-60F82A20AEF7"
	fileSaveDialogCLSID = "C0B4E2F3-BA21-4773-8DBA-335EC946EB8B"
	fileOpenDialogIID   = "D57C7288-D4AD-4768-BE02-9D969532D960"
	fileSaveDialogIID   = "84BCCD23-5FDE-4CDB-AEA4-AF64B83D78AB"
	sigdnFileSystemPath = 0x80058000
	fosOverwritePrompt  = 0x2
	fosPickFolders      = 0x20
	fosForceFileSystem  = 0x40
	fosAllowMultiSelect = 0x200
	fosPathMustExist    = 0x800
	fosFileMustExist    = 0x1000
	nimAdd              = 0
	nimModify           = 1
	nimDelete           = 2
	nifMessage          = 1
	nifIcon             = 2
	nifTip              = 4
	nifInfo             = 0x10
	trayIconID          = 1
	wmLButtonUp         = 0x0202
	wmRButtonUp         = 0x0205
	wmContextMenu       = 0x007B
)

var (
	capUser32 = syscall.NewLazyDLL("user32.dll")
	capKernel = syscall.NewLazyDLL("kernel32.dll")
	capShell  = syscall.NewLazyDLL("shell32.dll")
	capComDlg = syscall.NewLazyDLL("comctl32.dll")
)

type trayState struct {
	data    notifyIconData
	menu    uintptr
	actions map[string]menuAction
}

// Layout follows NOTIFYICONDATAW for Windows 7 and later (976 bytes on amd64).
type notifyIconData struct {
	Size        uint32
	Pad0        uint32
	Hwnd        uintptr
	ID          uint32
	Flags       uint32
	Callback    uint32
	Pad1        uint32
	Icon        uintptr
	Tip         [128]uint16
	State       uint32
	StateMask   uint32
	Info        [256]uint16
	TimeoutVer  uint32
	InfoTitle   [64]uint16
	InfoFlags   uint32
	GUID        [16]byte
	BalloonIcon uintptr
}

//go:uintptrescapes
func callWindows(dll *syscall.LazyDLL, name string, args ...uintptr) (uintptr, uintptr, error) {
	p := dll.NewProc(name)
	if err := p.Find(); err != nil {
		return 0, 0, err
	}
	r1, r2, err := syscall.SyscallN(p.Addr(), args...)
	if err != 0 {
		return r1, r2, err
	}
	return r1, r2, nil
}

func (s *winShell) SetTrayTitle(title string) error {
	s.mu.Lock()
	s.trayTitle = title
	tray := s.tray
	s.mu.Unlock()
	if tray == nil {
		return &desktop.Error{Code: desktop.CodeUnsupported, Message: "this app has no notification area icon"}
	}
	return s.Main(func() {
		tooltip := title
		if tooltip == "" && tray.data.Tip[0] == 0 {
			tooltip = "gofastr"
		}
		setWideBuffer(tray.data.Tip[:], tooltip)
		tray.data.Flags = nifTip
		_, _, _ = callWindows(capShell, "Shell_NotifyIconW", nimModify, uintptr(unsafe.Pointer(&tray.data)))
	})
}

func (s *winShell) installTray(cfg *desktop.Tray) {
	if cfg == nil {
		return
	}
	tray := &trayState{actions: make(map[string]menuAction)}
	tray.data = notifyIconData{Size: uint32(unsafe.Sizeof(notifyIconData{})), Hwnd: s.hwnd, ID: trayIconID,
		Flags: nifMessage | nifIcon | nifTip, Callback: wmTrayNotify}
	tray.data.Icon, _, _ = callWindows(capUser32, "LoadIconW", 0, uintptr(32512))
	tip := cfg.Tooltip
	if s.trayTitle != "" {
		tip = s.trayTitle
	} else if cfg.Title != "" {
		tip = cfg.Title
	}
	if tip == "" {
		tip = "gofastr"
	}
	setWideBuffer(tray.data.Tip[:], tip)
	_, _, _ = callWindows(capShell, "Shell_NotifyIconW", nimAdd, uintptr(unsafe.Pointer(&tray.data)))
	if cfg.Menu != nil {
		ids := append([]string(nil), s.actionIDs...)
		rows := desktop.PlanTrayMenu(cfg, s.title, &ids)
		s.actionIDs = ids
		firstID := uint16(1000 + len(s.menuActions))
		popup, err := win32.CreatePopupMenu()
		if err == nil {
			_ = s.appendRows(popup, rows, nil)
			tray.menu = popup
			for id, a := range s.menuActions {
				if id >= firstID && len(a.path) > 0 {
					tray.actions[pathKey(a.path)] = a
				}
			}
		}
	}
	s.mu.Lock()
	s.tray = tray
	s.mu.Unlock()
}

func (s *winShell) removeTrayIcon() {
	s.mu.RLock()
	tray := s.tray
	s.mu.RUnlock()
	if tray == nil {
		return
	}
	tray.data.Flags = 0
	_, _, _ = callWindows(capShell, "Shell_NotifyIconW", nimDelete, uintptr(unsafe.Pointer(&tray.data)))
}

func (s *winShell) onTrayMessage(event uintptr) {
	s.mu.RLock()
	tray := s.tray
	hwnd := s.hwnd
	s.mu.RUnlock()
	switch uint32(event) {
	case wmLButtonUp:
		s.mu.RLock()
		main := s.windowsByID[desktop.MainWindowID]
		s.mu.RUnlock()
		if main != nil {
			_ = main.Focus()
		}
	case wmRButtonUp, wmContextMenu:
		if tray == nil || tray.menu == 0 {
			return
		}
		x, y := win32.CursorPosition()
		win32.SetForegroundWindow(hwnd)
		cmd := win32.TrackPopupMenu(tray.menu, hwnd, int(x), int(y))
		if cmd != 0 {
			s.handleMenuCommand(uint16(cmd))
		}
	}
}

func (s *winShell) Notifier() desktop.Notifier { return s }
func (s *winShell) Show(ctx context.Context, n desktop.Notification) error {
	if err := ctx.Err(); err != nil {
		return desktop.ErrCancelled
	}
	s.notifyMu.Lock()
	s.notifyLog = append(s.notifyLog, n)
	if len(s.notifyLog) > 200 {
		s.notifyLog = append([]desktop.Notification(nil), s.notifyLog[len(s.notifyLog)-200:]...)
	}
	s.notifyMu.Unlock()
	s.mu.RLock()
	tray := s.tray
	s.mu.RUnlock()
	if tray == nil {
		return &desktop.Error{Code: desktop.CodeUnsupported, Message: "notifications need an app notification area icon"}
	}
	return s.Main(func() {
		tray.data.Flags = nifInfo
		setWideBuffer(tray.data.InfoTitle[:], n.Title)
		body := n.Body
		if n.Subtitle != "" {
			body = n.Subtitle + "\n" + body
		}
		setWideBuffer(tray.data.Info[:], body)
		tray.data.InfoFlags = 0
		_, _, _ = callWindows(capShell, "Shell_NotifyIconW", nimModify, uintptr(unsafe.Pointer(&tray.data)))
	})
}

func (s *winShell) Prompt(ctx context.Context, req desktop.PermissionRequest) (desktop.Decision, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return desktop.DecisionDeny, desktop.ErrCancelled
	}
	s.promptMu.Lock()
	if len(s.promptQueue) > 0 {
		d := s.promptQueue[0]
		s.promptQueue = s.promptQueue[1:]
		s.promptMu.Unlock()
		return d, nil
	}
	gate := s.promptGate
	if gate == nil {
		gate = make(chan struct{}, 1)
		s.promptGate = gate
	}
	s.promptMu.Unlock()
	select {
	case gate <- struct{}{}:
		defer func() { <-gate }()
	case <-ctx.Done():
		return desktop.DecisionDeny, desktop.ErrCancelled
	}
	var decision desktop.Decision
	err := s.runNativeModal(ctx, func(op *modalOperation) error {
		var promptErr error
		decision, promptErr = showPermissionPrompt(s.hwnd, req, op)
		return promptErr
	})
	if err != nil {
		return desktop.DecisionDeny, err
	}
	return decision, nil
}

func (s *winShell) queueOnMain(id uintptr, fn, cancel func()) (<-chan struct{}, error) {
	done := make(chan struct{})
	if win32.CurrentThreadID() == s.uiThreadID && s.uiThreadID != 0 {
		defer close(done)
		fn()
		return done, nil
	}
	entry := mainWork{
		run: func() {
			defer close(done)
			fn()
		},
		cancel: func() {
			if cancel != nil {
				cancel()
			}
			close(done)
		},
	}
	if err := s.enqueueWork(id, entry); err != nil {
		return nil, err
	}
	return done, nil
}

type modalOperation struct {
	shell             *winShell
	id                atomic.Uintptr
	cancelled         atomic.Bool
	finished          atomic.Bool
	dialogHWND        atomic.Uintptr
	taskDialogButtons *[36]byte // Keep the packed TASKDIALOG_BUTTON pointers on the heap.
}

func (op *modalOperation) cancel() {
	if op.finished.Load() {
		return
	}
	op.cancelled.Store(true)
	if hwnd := op.dialogHWND.Load(); hwnd != 0 {
		_ = win32.PostMessage(hwnd, wmClose, 0, 0)
		return
	}
	if op.shell != nil {
		op.shell.mu.RLock()
		hwnd := op.shell.hwnd
		op.shell.mu.RUnlock()
		if hwnd != 0 {
			_ = win32.PostMessage(hwnd, wmCancelModal, op.id.Load(), 0)
		}
	}
}

func (s *winShell) closePermissionDialog(owner uintptr, op *modalOperation) {
	hwnd := op.dialogHWND.Load()
	if hwnd == 0 {
		hwnd = win32.FindOwnedWindow(owner, "#32770", "Permission required")
	}
	if hwnd != 0 {
		_ = win32.PostMessage(hwnd, wmClose, 0, 0)
	}
}

func (s *winShell) runNativeModal(ctx context.Context, fn func(*modalOperation) error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return desktop.ErrCancelled
	}
	op := &modalOperation{shell: s}
	op.id.Store(s.workID.Add(1))
	stopWatcher := make(chan struct{})
	watcherDone := make(chan struct{})
	go func() {
		defer close(watcherDone)
		select {
		case <-ctx.Done():
			op.cancel()
		case <-stopWatcher:
		}
	}()

	var modalErr error
	done, err := s.queueOnMain(op.id.Load(), func() {
		defer op.finished.Store(true)
		if ctx.Err() != nil || op.cancelled.Load() {
			op.cancelled.Store(true)
			return
		}
		modalErr = fn(op)
	}, func() {
		op.cancelled.Store(true)
		op.finished.Store(true)
	})
	if err != nil {
		close(stopWatcher)
		<-watcherDone
		return err
	}
	select {
	case <-done:
		close(stopWatcher)
		<-watcherDone
		if ctx.Err() != nil || op.cancelled.Load() {
			return desktop.ErrCancelled
		}
		return modalErr
	case <-ctx.Done():
		op.cancel()
		<-done
		close(stopWatcher)
		<-watcherDone
		return desktop.ErrCancelled
	}
}

var activePermissionPrompts sync.Map // operation ID -> *modalOperation

var taskDialogCallback = syscall.NewCallback(func(hwnd, notification, _, _, data uintptr) uintptr {
	value, ok := activePermissionPrompts.Load(data)
	if !ok {
		return 0
	}
	op := value.(*modalOperation)
	switch notification {
	case 0: // TDN_CREATED
		op.dialogHWND.Store(hwnd)
		if op.cancelled.Load() {
			_ = win32.PostMessage(hwnd, wmClose, 0, 0)
		}
	case 5: // TDN_DESTROYED
		op.dialogHWND.Store(0)
	}
	return 0
})

type activationContext struct {
	Size                  uint32
	Flags                 uint32
	Source                *uint16
	ProcessorArchitecture uint16
	Language              uint16
	AssemblyDirectory     *uint16
	ResourceName          *uint16
	ApplicationName       *uint16
	Module                uintptr
}

func findTaskDialogProc() (*syscall.LazyProc, func(), error) {
	const manifest = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0">
  <assemblyIdentity type="win32" name="gofastr.desktop" version="1.0.0.0"/>
  <dependency><dependentAssembly>
    <assemblyIdentity type="win32" name="Microsoft.Windows.Common-Controls" version="6.0.0.0" processorArchitecture="*" publicKeyToken="6595b64144ccf1df" language="*"/>
  </dependentAssembly></dependency>
</assembly>`
	file, err := os.CreateTemp("", "gofastr-comctl-v6-*.manifest")
	if err != nil {
		return nil, nil, fmt.Errorf("create common-controls manifest: %w", err)
	}
	manifestPath := file.Name()
	removeManifest := func() { _ = os.Remove(manifestPath) }
	if _, err := file.WriteString(manifest); err != nil {
		_ = file.Close()
		removeManifest()
		return nil, nil, fmt.Errorf("write common-controls manifest: %w", err)
	}
	if err := file.Close(); err != nil {
		removeManifest()
		return nil, nil, fmt.Errorf("close common-controls manifest: %w", err)
	}
	source, err := win32.UTF16Ptr(manifestPath)
	if err != nil {
		removeManifest()
		return nil, nil, err
	}
	ctx := activationContext{Size: uint32(unsafe.Sizeof(activationContext{})), Source: source}
	handle, _, callErr := callWindows(capKernel, "CreateActCtxW", uintptr(unsafe.Pointer(&ctx)))
	runtime.KeepAlive(ctx)
	runtime.KeepAlive(source)
	if handle == ^uintptr(0) {
		removeManifest()
		if callErr != nil {
			return nil, nil, fmt.Errorf("create common-controls activation context: %w", callErr)
		}
		return nil, nil, errors.New("desktop/windows: create common-controls activation context failed")
	}
	var cookie uintptr
	activated, _, activateErr := callWindows(capKernel, "ActivateActCtx", handle, uintptr(unsafe.Pointer(&cookie)))
	if activated == 0 {
		_, _, _ = callWindows(capKernel, "ReleaseActCtx", handle)
		removeManifest()
		if activateErr != nil {
			return nil, nil, fmt.Errorf("activate common-controls context: %w", activateErr)
		}
		return nil, nil, errors.New("desktop/windows: activate common-controls context failed")
	}
	cleanup := func() {
		_, _, _ = callWindows(capKernel, "DeactivateActCtx", 0, cookie)
		_, _, _ = callWindows(capKernel, "ReleaseActCtx", handle)
		removeManifest()
	}
	proc := capComDlg.NewProc("TaskDialogIndirect")
	if err := proc.Find(); err != nil {
		cleanup()
		return nil, nil, err
	}
	return proc, cleanup, nil
}

func showPermissionMessageBox(owner uintptr, instruction string, op *modalOperation) (desktop.Decision, error) {
	if op.cancelled.Load() {
		return desktop.DecisionDeny, desktop.ErrCancelled
	}
	message := instruction + "\n\nSelect Yes to Allow, No to Allow Once, or Cancel to Deny."
	result := win32.MessageBox(owner, message, "Permission required", 0x3|0x20|0x2000)
	if op.cancelled.Load() {
		return desktop.DecisionDeny, desktop.ErrCancelled
	}
	switch result {
	case 6: // IDYES
		return desktop.DecisionAllow, nil
	case 7: // IDNO
		return desktop.DecisionAllowOnce, nil
	case 2: // IDCANCEL: deny and fail closed
		return desktop.DecisionDeny, nil
	default:
		return desktop.DecisionDeny, errors.New("desktop/windows: permission dialog could not be shown")
	}
}

func showPermissionPrompt(owner uintptr, req desktop.PermissionRequest, op *modalOperation) (desktop.Decision, error) {
	instruction := "Allow this app to use " + req.Capability + "."
	if req.Description != "" {
		instruction = req.Description
	}
	content := "Choose Allow to grant access, Allow Once for this request, or Deny."
	title, _ := win32.UTF16Ptr("Permission required")
	main, _ := win32.UTF16Ptr(instruction)
	body, _ := win32.UTF16Ptr(content)
	btnOnce, _ := win32.UTF16Ptr("Allow Once")
	btnAllow, _ := win32.UTF16Ptr("Allow")
	btnDeny, _ := win32.UTF16Ptr("Deny")
	buttons := new([36]byte)
	op.taskDialogButtons = buttons
	for i, button := range []struct {
		id   uint32
		text *uint16
	}{{101, btnOnce}, {102, btnAllow}, {103, btnDeny}} {
		offset := i * 12
		binary.LittleEndian.PutUint32(buttons[offset:offset+4], button.id)
		binary.LittleEndian.PutUint64(buttons[offset+4:offset+12], uint64(uintptr(unsafe.Pointer(button.text))))
	}
	var cfg [160]byte // TASKDIALOGCONFIG is packed to one-byte alignment.
	binary.LittleEndian.PutUint32(cfg[0:4], uint32(len(cfg)))
	binary.LittleEndian.PutUint64(cfg[4:12], uint64(owner))
	binary.LittleEndian.PutUint32(cfg[20:24], 0x1000|0x8) // relative to owner, allow cancellation
	binary.LittleEndian.PutUint64(cfg[28:36], uint64(uintptr(unsafe.Pointer(title))))
	binary.LittleEndian.PutUint64(cfg[44:52], uint64(uintptr(unsafe.Pointer(main))))
	binary.LittleEndian.PutUint64(cfg[52:60], uint64(uintptr(unsafe.Pointer(body))))
	binary.LittleEndian.PutUint32(cfg[60:64], 3)
	binary.LittleEndian.PutUint64(cfg[64:72], uint64(uintptr(unsafe.Pointer(&buttons[0]))))
	binary.LittleEndian.PutUint32(cfg[72:76], 101)
	binary.LittleEndian.PutUint64(cfg[140:148], uint64(taskDialogCallback))
	binary.LittleEndian.PutUint64(cfg[148:156], uint64(op.id.Load()))
	activePermissionPrompts.Store(op.id.Load(), op)
	defer activePermissionPrompts.Delete(op.id.Load())
	op.shell.registerModalCancel(op.id.Load(), func() { op.shell.closePermissionDialog(owner, op) })
	defer op.shell.unregisterModalCancel(op.id.Load())
	proc, deactivate, err := findTaskDialogProc()
	if err != nil {
		return showPermissionMessageBox(owner, instruction, op)
	}
	defer deactivate()
	var pressed int32
	r, _, _ := proc.Call(uintptr(unsafe.Pointer(&cfg[0])), uintptr(unsafe.Pointer(&pressed)), 0, 0)
	runtime.KeepAlive(buttons)
	runtime.KeepAlive(cfg)
	runtime.KeepAlive(title)
	runtime.KeepAlive(main)
	runtime.KeepAlive(body)
	runtime.KeepAlive(btnOnce)
	runtime.KeepAlive(btnAllow)
	runtime.KeepAlive(btnDeny)
	if int32(r) < 0 {
		return showPermissionMessageBox(owner, instruction, op)
	}
	if op.cancelled.Load() {
		return desktop.DecisionDeny, desktop.ErrCancelled
	}
	switch pressed {
	case 101:
		return desktop.DecisionAllowOnce, nil
	case 102:
		return desktop.DecisionAllow, nil
	case 103:
		return desktop.DecisionDeny, nil
	default:
		return desktop.DecisionDeny, desktop.ErrCancelled
	}
}

func setWideBuffer(dst []uint16, value string) {
	for i := range dst {
		dst[i] = 0
	}
	encoded, err := syscall.UTF16FromString(value)
	if err != nil {
		encoded = []uint16{}
	}
	copy(dst, encoded)
}

func (s *winShell) Clipboard() desktop.Clipboard { return s }
func (s *winShell) ReadText(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", desktop.ErrCancelled
	}
	var value string
	var clipErr error
	err := s.Main(func() {
		if e := openClipboard(s.hwnd); e != nil {
			clipErr = e
			return
		}
		defer callWindows(capUser32, "CloseClipboard")
		h, _, _ := callWindows(capUser32, "GetClipboardData", cfUnicodeText)
		if h == 0 {
			return
		}
		p, _, _ := callWindows(capKernel, "GlobalLock", h)
		if p == 0 {
			return
		}
		value = win32.ReadUTF16(p, 8<<20)
		_, _, _ = callWindows(capKernel, "GlobalUnlock", h)
	})
	if err != nil {
		return "", err
	}
	if clipErr != nil {
		return "", clipErr
	}
	return value, nil
}

func (s *winShell) WriteText(ctx context.Context, value string) error {
	if err := ctx.Err(); err != nil {
		return desktop.ErrCancelled
	}
	words, err := syscall.UTF16FromString(value)
	if err != nil {
		return err
	}
	var clipErr error
	mainErr := s.Main(func() {
		if e := openClipboard(s.hwnd); e != nil {
			clipErr = e
			return
		}
		defer callWindows(capUser32, "CloseClipboard")
		_, _, _ = callWindows(capUser32, "EmptyClipboard")
		h, _, _ := callWindows(capKernel, "GlobalAlloc", gmemMoveable|gmemZeroInit, uintptr(len(words)*2))
		if h == 0 {
			clipErr = errors.New("desktop/windows: could not allocate clipboard text")
			return
		}
		p, _, _ := callWindows(capKernel, "GlobalLock", h)
		if p == 0 {
			_, _, _ = callWindows(capKernel, "GlobalFree", h)
			clipErr = errors.New("desktop/windows: could not lock clipboard text")
			return
		}
		copy(unsafe.Slice((*uint16)(unsafe.Pointer(p)), len(words)), words)
		_, _, _ = callWindows(capKernel, "GlobalUnlock", h)
		if set, _, _ := callWindows(capUser32, "SetClipboardData", cfUnicodeText, h); set == 0 {
			_, _, _ = callWindows(capKernel, "GlobalFree", h)
			clipErr = errors.New("desktop/windows: could not transfer text to Windows clipboard")
		}
	})
	if mainErr != nil {
		return mainErr
	}
	return clipErr
}

func openClipboard(owner uintptr) error {
	for i := 0; i < 20; i++ {
		if r, _, _ := callWindows(capUser32, "OpenClipboard", owner); r != 0 {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return errors.New("desktop/windows: Windows clipboard is busy")
}

func (s *winShell) Dialogs() desktop.Dialogs { return s }
func (s *winShell) OpenFile(ctx context.Context, opts desktop.OpenOptions) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, desktop.ErrCancelled
	}
	var paths []string
	err := s.runNativeModal(ctx, func(op *modalOperation) error {
		var dialogErr error
		paths, dialogErr = showFileDialog(s, op, opts, nil, false)
		return dialogErr
	})
	if err != nil {
		return nil, err
	}
	return paths, nil
}

func (s *winShell) SaveFile(ctx context.Context, opts desktop.SaveOptions) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", desktop.ErrCancelled
	}
	var path string
	err := s.runNativeModal(ctx, func(op *modalOperation) error {
		var dialogErr error
		path, dialogErr = showSaveDialog(s, op, opts)
		return dialogErr
	})
	if err != nil {
		return "", err
	}
	return path, nil
}

func (s *winShell) OpenFolder(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", desktop.ErrCancelled
	}
	var path string
	err := s.runNativeModal(ctx, func(op *modalOperation) error {
		var dialogErr error
		path, dialogErr = showFolderDialog(s, op)
		return dialogErr
	})
	if err != nil {
		return "", err
	}
	return path, nil
}

type fileFilterSpec struct{ Name, Pattern *uint16 }

func showFileDialog(s *winShell, op *modalOperation, opts desktop.OpenOptions, save *desktop.SaveOptions, folder bool) ([]string, error) {
	s.mu.RLock()
	hwnd := s.hwnd
	s.mu.RUnlock()
	iid := fileOpenDialogIID
	clsid := fileOpenDialogCLSID
	if save != nil {
		iid = fileSaveDialogIID
		clsid = fileSaveDialogCLSID
	}
	dlg, err := win32.CreateInstance(clsid, iid)
	if err != nil {
		return nil, err
	}
	defer win32.Release(dlg)
	s.registerModalCancel(op.id.Load(), func() {
		_, _ = win32.COMCall(dlg, 23 /* IFileDialog.Close */, 0x800704C7 /* ERROR_CANCELLED */)
	})
	defer s.unregisterModalCancel(op.id.Load())
	if op.cancelled.Load() {
		return nil, desktop.ErrCancelled
	}
	var currentFlags uint32
	_, _ = win32.COMCall(dlg, 10, uintptr(unsafe.Pointer(&currentFlags)))
	flags := currentFlags | uint32(fosForceFileSystem|fosPathMustExist)
	if save != nil {
		flags |= fosOverwritePrompt
	} else {
		flags |= fosFileMustExist
	}
	if opts.Multiple {
		flags |= fosAllowMultiSelect
	}
	if folder {
		flags |= fosPickFolders
		flags &^= fosFileMustExist
	}
	if _, err := win32.COMCall(dlg, 9, uintptr(flags)); err != nil {
		return nil, err
	}
	var filterValues []fileFilterSpec
	for _, filter := range opts.Filters {
		name, _ := win32.UTF16Ptr(filter.Name)
		parts := make([]string, 0, len(filter.Extensions))
		for _, ext := range filter.Extensions {
			ext = strings.TrimPrefix(ext, ".")
			if ext != "" {
				parts = append(parts, "*."+ext)
			}
		}
		pattern, _ := win32.UTF16Ptr(strings.Join(parts, ";"))
		filterValues = append(filterValues, fileFilterSpec{Name: name, Pattern: pattern})
	}
	if len(filterValues) != 0 {
		_, _ = win32.COMCall(dlg, 4, uintptr(len(filterValues)), uintptr(unsafe.Pointer(&filterValues[0])))
	}
	var defaultName *uint16
	if save != nil && save.DefaultName != "" {
		defaultName, _ = win32.UTF16Ptr(save.DefaultName)
		_, _ = win32.COMCall(dlg, 15, uintptr(unsafe.Pointer(defaultName)))
	}
	if op.cancelled.Load() {
		return nil, desktop.ErrCancelled
	}
	showResult, showErr := win32.COMCall(dlg, 3, hwnd)
	if op.cancelled.Load() || uint32(showResult) == 0x800704C7 {
		return nil, desktop.ErrCancelled
	}
	if showErr != nil {
		return nil, showErr
	}
	runtime.KeepAlive(filterValues)
	runtime.KeepAlive(defaultName)
	if save != nil || folder {
		item, err := getDialogResultPath(dlg)
		if err != nil {
			return nil, err
		}
		return []string{item}, nil
	}
	if opts.Multiple {
		var array uintptr
		if _, err := win32.COMCall(dlg, 27, uintptr(unsafe.Pointer(&array))); err != nil {
			return nil, err
		}
		defer win32.Release(array)
		return shellItemArrayPaths(array)
	}
	path, err := getDialogResultPath(dlg)
	if err != nil {
		return nil, err
	}
	return []string{path}, nil
}

type shellItemArrayAPI struct {
	getCount  func(uintptr, *uint32) error
	getItemAt func(uintptr, uint32, *uintptr) error
	release   func(uintptr)
	path      func(uintptr) (string, error)
}

const (
	shellItemArrayGetCountSlot  = 7
	shellItemArrayGetItemAtSlot = 8
)

func shellItemArrayPaths(array uintptr) ([]string, error) {
	return readShellItemArray(array, shellItemArrayAPI{
		getCount: func(array uintptr, count *uint32) error {
			_, err := win32.COMCall(array, shellItemArrayGetCountSlot, uintptr(unsafe.Pointer(count)))
			return err
		},
		getItemAt: func(array uintptr, index uint32, item *uintptr) error {
			_, err := win32.COMCall(array, shellItemArrayGetItemAtSlot, uintptr(index), uintptr(unsafe.Pointer(item)))
			return err
		},
		release: win32.Release,
		path:    shellItemPath,
	})
}

func readShellItemArray(array uintptr, api shellItemArrayAPI) ([]string, error) {
	var count uint32
	if err := api.getCount(array, &count); err != nil {
		return nil, err
	}
	paths := make([]string, 0, count)
	for i := uint32(0); i < count; i++ {
		var item uintptr
		if err := api.getItemAt(array, i, &item); err != nil {
			return nil, err
		}
		p, err := api.path(item)
		api.release(item)
		if err != nil {
			return nil, err
		}
		paths = append(paths, p)
	}
	return paths, nil
}

func showSaveDialog(s *winShell, op *modalOperation, opts desktop.SaveOptions) (string, error) {
	paths, err := showFileDialog(s, op, desktop.OpenOptions{}, &opts, false)
	if err != nil || len(paths) == 0 {
		return "", err
	}
	return paths[0], nil
}

func showFolderDialog(s *winShell, op *modalOperation) (string, error) {
	paths, err := showFileDialog(s, op, desktop.OpenOptions{}, nil, true)
	if err != nil || len(paths) == 0 {
		return "", err
	}
	return paths[0], nil
}

func getDialogResultPath(dlg uintptr) (string, error) {
	var item uintptr
	if _, err := win32.COMCall(dlg, 20, uintptr(unsafe.Pointer(&item))); err != nil {
		return "", err
	}
	defer win32.Release(item)
	return shellItemPath(item)
}

func shellItemPath(item uintptr) (string, error) {
	var p uintptr
	if _, err := win32.COMCall(item, 5, sigdnFileSystemPath, uintptr(unsafe.Pointer(&p))); err != nil {
		return "", err
	}
	defer win32.CoTaskMemFree(p)
	return win32.ReadUTF16(p, 32768), nil
}

func waitWindows(ms int) { time.Sleep(time.Duration(ms) * time.Millisecond) }
