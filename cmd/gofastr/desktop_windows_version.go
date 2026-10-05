//go:build windows

package main

import (
	"encoding/binary"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"unicode/utf16"
	"unsafe"
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

func windowsVersionParts(version string) ([4]uint16, error) {
	var parts [4]uint16
	if !desktopVersionRe.MatchString(version) {
		return parts, fmt.Errorf("--version %q must match %s", version, desktopVersionRe.String())
	}
	if len(version) > 256 {
		return parts, fmt.Errorf("--version is longer than 256 characters")
	}
	components := strings.Split(version, ".")
	if len(components) > len(parts) {
		return parts, fmt.Errorf("--version has more than four components; Windows file versions support at most four")
	}
	for i, component := range components {
		end := 0
		for end < len(component) && component[end] >= '0' && component[end] <= '9' {
			end++
		}
		if end == 0 {
			continue
		}
		n, err := strconv.ParseUint(component[:end], 10, 16)
		if err != nil {
			return parts, fmt.Errorf("--version component %q does not fit in a Windows file version: %w", component, err)
		}
		parts[i] = uint16(n)
	}
	return parts, nil
}

func renderWindowsVersionResource(info windowsDesktopVersion, version [4]uint16) []byte {
	stringsTable := versionInfoBlock("040904B0", nil, 0, 1,
		versionString("CompanyName", "GoFastr"),
		versionString("FileDescription", info.Name),
		versionString("FileVersion", info.Version),
		versionString("InternalName", info.Name),
		versionString("OriginalFilename", info.Name+".exe"),
		versionString("ProductName", info.Name),
		versionString("ProductVersion", info.Version),
		versionString("Comments", "Application ID: "+info.ID),
	)
	stringFileInfo := versionInfoBlock("StringFileInfo", nil, 0, 1, stringsTable)
	translation := []byte{0x09, 0x04, 0xB0, 0x04} // LANG_ENGLISH/US, Unicode
	translationEntry := versionInfoBlock("Translation", translation, uint16(len(translation)), 0)
	varFileInfo := versionInfoBlock("VarFileInfo", nil, 0, 1, translationEntry)

	fileVersionMS := uint32(version[0])<<16 | uint32(version[1])
	fileVersionLS := uint32(version[2])<<16 | uint32(version[3])
	fixed := make([]byte, 0, 52)
	for _, word := range []uint32{
		0xFEEF04BD, // dwSignature
		0x00010000, // dwStrucVersion
		fileVersionMS,
		fileVersionLS,
		fileVersionMS,
		fileVersionLS,
		0x3F,       // dwFileFlagsMask
		0,          // dwFileFlags
		0x00040004, // VOS_NT_WINDOWS32
		1,          // VFT_APP
		0,          // dwFileSubtype
		0,          // dwFileDateMS
		0,          // dwFileDateLS
	} {
		fixed = binary.LittleEndian.AppendUint32(fixed, word)
	}
	return versionInfoBlock("VS_VERSION_INFO", fixed, uint16(len(fixed)), 0,
		stringFileInfo, varFileInfo)
}

func versionString(key, value string) []byte {
	encoded := utf16.Encode([]rune(value))
	encoded = append(encoded, 0)
	data := make([]byte, 0, len(encoded)*2)
	for _, unit := range encoded {
		data = binary.LittleEndian.AppendUint16(data, unit)
	}
	return versionInfoBlock(key, data, uint16(len(encoded)), 1)
}

func versionInfoBlock(key string, value []byte, valueLength, valueType uint16, children ...[]byte) []byte {
	block := make([]byte, 6, 6+len(key)*2+len(value))
	for _, unit := range utf16.Encode([]rune(key + "\x00")) {
		block = binary.LittleEndian.AppendUint16(block, unit)
	}
	alignVersionInfo(&block)
	block = append(block, value...)
	alignVersionInfo(&block)
	for _, child := range children {
		block = append(block, child...)
		alignVersionInfo(&block)
	}
	if len(block) > 0xFFFF {
		panic("desktop: Windows version resource block exceeds 65535 bytes")
	}
	binary.LittleEndian.PutUint16(block[0:2], uint16(len(block)))
	binary.LittleEndian.PutUint16(block[2:4], valueLength)
	binary.LittleEndian.PutUint16(block[4:6], valueType)
	return block
}

func alignVersionInfo(block *[]byte) {
	for len(*block)%4 != 0 {
		*block = append(*block, 0)
	}
}
