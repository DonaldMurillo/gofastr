//go:build windows && amd64

package win32

import (
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"
)

const (
	COINIT_APARTMENTTHREADED = 0x2
	CLSCTX_INPROC_SERVER     = 0x1
	S_OK                     = 0
	E_NOINTERFACE            = 0x80004002
	E_POINTER                = 0x80004003
	E_FAIL                   = 0x80004005
)

var (
	ole32        = syscall.NewLazyDLL("ole32.dll")
	kernel       = syscall.NewLazyDLL("kernel32.dll")
	callbackOnce sync.Once
	comVTable    [4]uintptr
	comMu        sync.RWMutex
	comHandlers  = map[uintptr]func(uintptr, uintptr) uintptr{}
	comIDs       = map[uintptr]string{}
	comNextID    atomic.Uintptr
)

type comHeader struct {
	VTable uintptr
	ID     uintptr
	Refs   uintptr
}

func ensureCOMCallbacks() {
	callbackOnce.Do(func() {
		comVTable = [4]uintptr{
			syscall.NewCallback(comQueryInterface),
			syscall.NewCallback(comAddRef),
			syscall.NewCallback(comRelease),
			syscall.NewCallback(comInvoke),
		}
	})
}

// NewCOMHandler creates a native IUnknown object with the requested
// interface IID. The callback receives the first and second Invoke
// parameters after this (commonly HRESULT and an interface pointer).
func NewCOMHandler(iid string, invoke func(arg1, arg2 uintptr) uintptr) (uintptr, error) {
	ensureCOMCallbacks()
	proc := kernel.NewProc("GetProcessHeap")
	heap, _, _ := proc.Call()
	alloc := kernel.NewProc("HeapAlloc")
	mem, _, err := alloc.Call(heap, 0x8 /* HEAP_ZERO_MEMORY */, unsafe.Sizeof(comHeader{}))
	if mem == 0 {
		return 0, fmt.Errorf("HeapAlloc for COM callback: %w", lastError(err))
	}
	id := comNextID.Add(1)
	if id == 0 {
		id = comNextID.Add(1)
	}
	h := (*comHeader)(unsafe.Pointer(mem))
	h.VTable = uintptr(unsafe.Pointer(&comVTable[0]))
	h.ID = id
	h.Refs = 1
	comMu.Lock()
	comHandlers[id] = invoke
	comIDs[id] = normalizeGUID(iid)
	comMu.Unlock()
	return mem, nil
}

func comQueryInterface(this, riid, out uintptr) uintptr {
	if out == 0 || this == 0 || riid == 0 {
		return E_POINTER
	}
	*(*uintptr)(unsafe.Pointer(out)) = 0
	h := (*comHeader)(unsafe.Pointer(this))
	comMu.RLock()
	want := comIDs[h.ID]
	comMu.RUnlock()
	got := guidFromMemory(riid)
	if got != "0000000000000000c000000000000046" && got != want {
		return E_NOINTERFACE
	}
	*(*uintptr)(unsafe.Pointer(out)) = this
	comAddRef(this)
	return S_OK
}

func comAddRef(this uintptr) uintptr {
	if this == 0 {
		return 0
	}
	h := (*comHeader)(unsafe.Pointer(this))
	return atomic.AddUintptr(&h.Refs, 1)
}

func comRelease(this uintptr) uintptr {
	if this == 0 {
		return 0
	}
	h := (*comHeader)(unsafe.Pointer(this))
	left := atomic.AddUintptr(&h.Refs, ^uintptr(0))
	if left == 0 {
		id := h.ID
		comMu.Lock()
		delete(comHandlers, id)
		delete(comIDs, id)
		comMu.Unlock()
		heapProc := kernel.NewProc("GetProcessHeap")
		heap, _, _ := heapProc.Call()
		free := kernel.NewProc("HeapFree")
		_, _, _ = free.Call(heap, 0, this)
	}
	return left
}

func comInvoke(this, arg1, arg2 uintptr) uintptr {
	if this == 0 {
		return E_POINTER
	}
	h := (*comHeader)(unsafe.Pointer(this))
	comMu.RLock()
	fn := comHandlers[h.ID]
	comMu.RUnlock()
	if fn == nil {
		return E_FAIL
	}
	return fn(arg1, arg2)
}

// Release drops a COM interface reference.
func Release(obj uintptr) {
	if obj != 0 {
		vtable := *(*uintptr)(unsafe.Pointer(obj))
		fn := *(*uintptr)(unsafe.Pointer(vtable + 2*unsafe.Sizeof(uintptr(0))))
		_, _, _ = syscall.SyscallN(fn, obj)
	}
}

// COMCall invokes an IUnknown-style interface method at vtable slot.
func COMCall(obj uintptr, slot int, args ...uintptr) (uintptr, error) {
	if obj == 0 {
		return 0, fmt.Errorf("COM call on nil interface")
	}
	vtable := *(*uintptr)(unsafe.Pointer(obj))
	fn := *(*uintptr)(unsafe.Pointer(vtable + uintptr(slot)*unsafe.Sizeof(uintptr(0))))
	all := append([]uintptr{obj}, args...)
	r, _, _ := syscall.SyscallN(fn, all...)
	if int32(r) < 0 {
		return r, HRESULTError(int32(r))
	}
	return r, nil
}

// QueryInterface returns a new reference for iid or an HRESULT error.
func QueryInterface(obj uintptr, iid string) (uintptr, error) {
	g, err := ParseGUID(iid)
	if err != nil {
		return 0, err
	}
	var out uintptr
	_, err = COMCall(obj, 0, uintptr(unsafe.Pointer(&g)), uintptr(unsafe.Pointer(&out)))
	if err != nil {
		return 0, err
	}
	return out, nil
}

func HRESULTError(hr int32) error {
	return fmt.Errorf("HRESULT 0x%08x", uint32(hr))
}

func CoInitializeSTA() error {
	p := ole32.NewProc("CoInitializeEx")
	r, _, _ := p.Call(0, COINIT_APARTMENTTHREADED)
	// S_FALSE is success and still requires CoUninitialize.
	if int32(r) < 0 {
		return HRESULTError(int32(r))
	}
	return nil
}
func CoUninitialize() { _, _, _ = ole32.NewProc("CoUninitialize").Call() }

type GUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

func ParseGUID(s string) (GUID, error) {
	var g GUID
	s = strings.Trim(strings.TrimSpace(s), "{}")
	parts := strings.Split(s, "-")
	if len(parts) != 5 || len(parts[0]) != 8 || len(parts[1]) != 4 || len(parts[2]) != 4 || len(parts[3]) != 4 || len(parts[4]) != 12 {
		return g, fmt.Errorf("invalid GUID %q", s)
	}
	parse := func(x string, dst []byte) error {
		b, err := hex.DecodeString(x)
		if err != nil || len(b) != len(dst) {
			return fmt.Errorf("invalid GUID %q", s)
		}
		copy(dst, b)
		return nil
	}
	var err error
	var tmp [4]byte
	if err = parse(parts[0], tmp[:]); err != nil {
		return g, err
	}
	g.Data1 = uint32(tmp[0])<<24 | uint32(tmp[1])<<16 | uint32(tmp[2])<<8 | uint32(tmp[3])
	var t2 [2]byte
	if err = parse(parts[1], t2[:]); err != nil {
		return g, err
	}
	g.Data2 = uint16(t2[0])<<8 | uint16(t2[1])
	if err = parse(parts[2], t2[:]); err != nil {
		return g, err
	}
	g.Data3 = uint16(t2[0])<<8 | uint16(t2[1])
	var tail [8]byte
	if err = parse(parts[3]+parts[4], tail[:]); err != nil {
		return g, err
	}
	g.Data4 = tail
	return g, nil
}

func normalizeGUID(s string) string {
	g, err := ParseGUID(s)
	if err != nil {
		return ""
	}
	b := (*[16]byte)(unsafe.Pointer(&g))
	return hex.EncodeToString(b[:])
}

func guidFromMemory(p uintptr) string {
	g := *(*GUID)(unsafe.Pointer(p))
	b := (*[16]byte)(unsafe.Pointer(&g))
	return hex.EncodeToString(b[:])
}

func CreateCoreWebView2EnvironmentWithOptions(loader uintptr, userData string, handler uintptr) error {
	if loader == 0 {
		return fmt.Errorf("WebView2Loader.dll is not loaded")
	}
	name, _ := syscall.BytePtrFromString("CreateCoreWebView2EnvironmentWithOptions")
	proc, _, procErr := syscall.SyscallN(kernel.NewProc("GetProcAddress").Addr(), loader, uintptr(unsafe.Pointer(name)))
	if proc == 0 {
		return fmt.Errorf("WebView2Loader.dll export CreateCoreWebView2EnvironmentWithOptions: %w", procErr)
	}
	var folder *uint16
	var err error
	if userData != "" {
		folder, err = syscall.UTF16PtrFromString(userData)
		if err != nil {
			return err
		}
	}
	r, _, _ := syscall.SyscallN(proc, 0, uintptr(unsafe.Pointer(folder)), 0, handler)
	if int32(r) < 0 {
		return HRESULTError(int32(r))
	}
	return nil
}

func LoadLibrary(path string) (uintptr, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	r, _, e := syscall.SyscallN(kernel.NewProc("LoadLibraryW").Addr(), uintptr(unsafe.Pointer(p)))
	if r == 0 {
		return 0, fmt.Errorf("LoadLibraryW(%s): %w", path, lastError(e))
	}
	return r, nil
}

func CreateStreamOnHGlobal() (uintptr, error) {
	var stream uintptr
	proc := ole32.NewProc("CreateStreamOnHGlobal")
	r, _, _ := proc.Call(0, 1, uintptr(unsafe.Pointer(&stream)))
	if int32(r) < 0 {
		return 0, HRESULTError(int32(r))
	}
	return stream, nil
}

func HGlobalFromStream(stream uintptr) uintptr {
	var h uintptr
	r, _, _ := ole32.NewProc("GetHGlobalFromStream").Call(stream, uintptr(unsafe.Pointer(&h)))
	if int32(r) < 0 {
		return 0
	}
	return h
}

func CopyHGlobal(h uintptr) []byte {
	if h == 0 {
		return nil
	}
	size, _, _ := kernel.NewProc("GlobalSize").Call(h)
	if size == 0 || size > 128<<20 {
		return nil
	}
	p, _, _ := kernel.NewProc("GlobalLock").Call(h)
	if p == 0 {
		return nil
	}
	out := append([]byte(nil), unsafe.Slice((*byte)(unsafe.Pointer(p)), int(size))...)
	_, _, _ = kernel.NewProc("GlobalUnlock").Call(h)
	return out
}

func CreateInstance(clsid, iid string) (uintptr, error) {
	c, err := ParseGUID(clsid)
	if err != nil {
		return 0, err
	}
	i, err := ParseGUID(iid)
	if err != nil {
		return 0, err
	}
	var out uintptr
	r, _, _ := ole32.NewProc("CoCreateInstance").Call(
		uintptr(unsafe.Pointer(&c)), 0, CLSCTX_INPROC_SERVER, uintptr(unsafe.Pointer(&i)), uintptr(unsafe.Pointer(&out)))
	if int32(r) < 0 {
		return 0, HRESULTError(int32(r))
	}
	return out, nil
}

func CoTaskMemFree(p uintptr) { _, _, _ = ole32.NewProc("CoTaskMemFree").Call(p) }

func ReadUTF16(p uintptr, max int) string {
	if p == 0 {
		return ""
	}
	words := unsafe.Slice((*uint16)(unsafe.Pointer(p)), max)
	for i, w := range words {
		if w == 0 {
			return syscall.UTF16ToString(words[:i])
		}
	}
	return syscall.UTF16ToString(words)
}

func UTF16Ptr(s string) (*uint16, error) { return syscall.UTF16PtrFromString(s) }
