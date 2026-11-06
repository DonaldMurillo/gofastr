//go:build windows

package desktopcmd

import (
	"encoding/binary"
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"github.com/DonaldMurillo/gofastr/internal/winversion"
)

type windowsDesktopVersion struct {
	Name    string
	ID      string
	Version string
}

type windowsResource struct {
	Type     uint16
	ID       uint16
	Language uint16
	Data     []byte
}

func writeWindowsVersionResource(path string, info windowsDesktopVersion) error {
	version, err := windowsVersionParts(info.Version)
	if err != nil {
		return err
	}
	data := renderWindowsVersionResource(info, version)
	return updateWindowsResources(path, []windowsResource{{Type: 16, ID: 1, Language: 0x0409, Data: data}})
}

func writeWindowsIconResource(path string, ico []byte) error {
	resources, err := windowsIconResources(ico)
	if err != nil {
		return err
	}
	return updateWindowsResources(path, resources)
}

func windowsIconResources(ico []byte) ([]windowsResource, error) {
	if len(ico) < 6 || binary.LittleEndian.Uint16(ico[0:2]) != 0 || binary.LittleEndian.Uint16(ico[2:4]) != 1 {
		return nil, fmt.Errorf("invalid ICO header")
	}
	count := int(binary.LittleEndian.Uint16(ico[4:6]))
	if count == 0 || len(ico) < 6+count*16 {
		return nil, fmt.Errorf("invalid ICO directory")
	}
	group := append([]byte(nil), ico[:6]...)
	resources := make([]windowsResource, 0, count+1)
	directoryEnd := 6 + count*16
	for i := 0; i < count; i++ {
		entry := ico[6+i*16 : 6+(i+1)*16]
		length := uint64(binary.LittleEndian.Uint32(entry[8:12]))
		offset := uint64(binary.LittleEndian.Uint32(entry[12:16]))
		if length == 0 || offset < uint64(directoryEnd) || offset+length > uint64(len(ico)) {
			return nil, fmt.Errorf("invalid ICO frame %d payload range", i)
		}
		id := uint16(i + 1)
		group = append(group, entry[:12]...)
		group = binary.LittleEndian.AppendUint16(group, id)
		resources = append(resources, windowsResource{
			Type: 3, ID: id, Language: 0,
			Data: ico[int(offset):int(offset+length)],
		})
	}
	resources = append(resources, windowsResource{Type: 14, ID: 1, Language: 0, Data: group})
	return resources, nil
}

func updateWindowsResources(path string, resources []windowsResource) error {
	if len(resources) == 0 {
		return fmt.Errorf("no Windows resources to write")
	}
	widePath, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}

	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	begin := kernel32.NewProc("BeginUpdateResourceW")
	update := kernel32.NewProc("UpdateResourceW")
	end := kernel32.NewProc("EndUpdateResourceW")
	for _, proc := range []*syscall.LazyProc{begin, update, end} {
		if err := proc.Find(); err != nil {
			return err
		}
	}
	handle, _, callErr := begin.Call(uintptr(unsafe.Pointer(widePath)), 0)
	runtime.KeepAlive(widePath)
	if handle == 0 {
		return windowsResourceCallError("BeginUpdateResourceW", callErr)
	}
	committed := false
	defer func() {
		if !committed {
			_, _, _ = end.Call(handle, 1) // discard the incomplete update
		}
	}()
	for _, resource := range resources {
		if len(resource.Data) == 0 {
			return fmt.Errorf("Windows resource type %d id %d has no data", resource.Type, resource.ID)
		}
		updated, _, callErr := update.Call(handle, uintptr(resource.Type), uintptr(resource.ID), uintptr(resource.Language),
			uintptr(unsafe.Pointer(&resource.Data[0])), uintptr(len(resource.Data)))
		runtime.KeepAlive(resource.Data)
		if updated == 0 {
			return windowsResourceCallError("UpdateResourceW", callErr)
		}
	}
	result, _, callErr := end.Call(handle, 0)
	committed = result != 0
	if !committed {
		return windowsResourceCallError("EndUpdateResourceW", callErr)
	}
	return nil
}

func windowsResourceCallError(name string, err error) error {
	if err == nil || err == syscall.Errno(0) {
		err = syscall.GetLastError()
	}
	return fmt.Errorf("%s: %w", name, err)
}

func renderWindowsVersionResource(info windowsDesktopVersion, version [4]uint16) []byte {
	return winversion.Render(winversion.Info{Name: info.Name, ID: info.ID, Version: info.Version, Parts: version})
}
