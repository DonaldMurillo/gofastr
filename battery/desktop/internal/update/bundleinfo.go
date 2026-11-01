package update

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
)

// ErrBadBundleInfo is the sentinel for an Info.plist the updater cannot
// read as the bundle's identity: missing, oversized, binary, or without
// a string CFBundleIdentifier and CFBundleShortVersionString.
var ErrBadBundleInfo = errors.New("bundle Info.plist is unreadable")

// maxInfoPlistSize caps one Info.plist read. `gofastr desktop build`
// writes about 1.5 KiB.
const maxInfoPlistSize = 1 << 20

// infoPlistPath is the Info.plist location inside a .app bundle.
const infoPlistPath = "Contents/Info.plist"

// BundleInfo is the identity a bundle's Info.plist declares.
type BundleInfo struct {
	Identifier   string // CFBundleIdentifier
	ShortVersion string // CFBundleShortVersionString
}

// ParseInfoPlist reads the identity keys from an XML property list (the
// format `gofastr desktop build` writes). A binary plist, a key whose
// value is not a <string>, a repeated identity key, or a missing one is
// refused: a bundle whose identity is ambiguous is not installed.
func ParseInfoPlist(data []byte) (BundleInfo, error) {
	if len(data) > maxInfoPlistSize {
		return BundleInfo{}, fmt.Errorf("%w: larger than %d bytes", ErrBadBundleInfo, maxInfoPlistSize)
	}
	if bytes.HasPrefix(data, []byte("bplist")) {
		return BundleInfo{}, fmt.Errorf("%w: binary property lists are not read; ship the XML Info.plist gofastr desktop build writes", ErrBadBundleInfo)
	}
	dec := xml.NewDecoder(bytes.NewReader(data))
	var info BundleInfo
	seen := map[string]bool{}
	depth, tops := 0, 0
	key := ""
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return BundleInfo{}, fmt.Errorf("%w: not well-formed XML", ErrBadBundleInfo)
		}
		switch el := tok.(type) {
		case xml.EndElement:
			depth--
		case xml.StartElement:
			depth++
			switch depth {
			case 1:
				if el.Name.Local != "plist" {
					return BundleInfo{}, fmt.Errorf("%w: the root element is not <plist>", ErrBadBundleInfo)
				}
			case 2:
				tops++
				if el.Name.Local != "dict" || tops > 1 {
					return BundleInfo{}, fmt.Errorf("%w: the plist does not hold one top-level <dict>", ErrBadBundleInfo)
				}
			default:
				// depth 3 is a top-level dict member; DecodeElement and
				// Skip consume the element through its end tag, so
				// deeper elements are never seen here.
				var err error
				key, err = plistMember(dec, el, key, &info, seen)
				if err != nil {
					return BundleInfo{}, err
				}
				depth--
			}
		}
	}
	if !seen["CFBundleIdentifier"] || !seen["CFBundleShortVersionString"] {
		return BundleInfo{}, fmt.Errorf("%w: CFBundleIdentifier or CFBundleShortVersionString is missing", ErrBadBundleInfo)
	}
	return info, nil
}

// plistMember consumes one top-level dict member: a <key>, or the value
// that follows the pending key. It returns the key still pending.
func plistMember(dec *xml.Decoder, el xml.StartElement, key string, info *BundleInfo, seen map[string]bool) (string, error) {
	if el.Name.Local == "key" {
		if key != "" {
			return "", fmt.Errorf("%w: a <key> has no value", ErrBadBundleInfo)
		}
		var name string
		if err := dec.DecodeElement(&name, &el); err != nil {
			return "", fmt.Errorf("%w: unreadable <key>", ErrBadBundleInfo)
		}
		if name == "" {
			return "", fmt.Errorf("%w: an empty <key>", ErrBadBundleInfo)
		}
		return name, nil
	}
	if key == "" {
		return "", fmt.Errorf("%w: a value has no <key>", ErrBadBundleInfo)
	}
	var dst *string
	switch key {
	case "CFBundleIdentifier":
		dst = &info.Identifier
	case "CFBundleShortVersionString":
		dst = &info.ShortVersion
	}
	if dst == nil {
		if err := dec.Skip(); err != nil {
			return "", fmt.Errorf("%w: not well-formed XML", ErrBadBundleInfo)
		}
		return "", nil
	}
	if seen[key] {
		return "", fmt.Errorf("%w: %s appears twice", ErrBadBundleInfo, key)
	}
	seen[key] = true
	if el.Name.Local != "string" {
		return "", fmt.Errorf("%w: %s is not a <string>", ErrBadBundleInfo, key)
	}
	if err := dec.DecodeElement(dst, &el); err != nil {
		return "", fmt.Errorf("%w: unreadable %s", ErrBadBundleInfo, key)
	}
	if *dst == "" {
		return "", fmt.Errorf("%w: %s is empty", ErrBadBundleInfo, key)
	}
	return "", nil
}

// ReadBundleInfo reads appDir/Contents/Info.plist. The read goes through
// os.Root, so a symlink in the bundle cannot point it outside appDir.
func ReadBundleInfo(appDir string) (BundleInfo, error) {
	root, err := os.OpenRoot(appDir)
	if err != nil {
		return BundleInfo{}, fmt.Errorf("%w: the bundle directory is unreadable", ErrBadBundleInfo)
	}
	defer root.Close()
	f, err := root.Open(infoPlistPath)
	if err != nil {
		return BundleInfo{}, fmt.Errorf("%w: the bundle has no %s", ErrBadBundleInfo, infoPlistPath)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxInfoPlistSize+1))
	if err != nil {
		return BundleInfo{}, fmt.Errorf("%w: read failed", ErrBadBundleInfo)
	}
	return ParseInfoPlist(data)
}

// zipBundleInfo reads <root>/Contents/Info.plist out of a zip archive.
// Two entries cleaning to that name are refused: which one an extractor
// keeps is not this function's call to make.
func zipBundleInfo(data []byte, root string) (BundleInfo, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return BundleInfo{}, fmt.Errorf("%w: not a zip archive", ErrBadArchive)
	}
	want := root + "/" + infoPlistPath
	var entry *zip.File
	for _, f := range zr.File {
		name, ok := cleanEntryName(f.Name)
		if !ok || name != want {
			continue
		}
		if entry != nil {
			return BundleInfo{}, fmt.Errorf("%w: the archive holds %s twice", ErrBadBundleInfo, want)
		}
		entry = f
	}
	if entry == nil || !entry.Mode().IsRegular() {
		return BundleInfo{}, fmt.Errorf("%w: the archive has no %s", ErrBadBundleInfo, want)
	}
	if entry.UncompressedSize64 > maxInfoPlistSize {
		return BundleInfo{}, fmt.Errorf("%w: larger than %d bytes", ErrBadBundleInfo, maxInfoPlistSize)
	}
	rc, err := entry.Open()
	if err != nil {
		return BundleInfo{}, fmt.Errorf("%w: unreadable entry", ErrBadBundleInfo)
	}
	defer rc.Close()
	plist, err := io.ReadAll(io.LimitReader(rc, maxInfoPlistSize+1))
	if err != nil {
		return BundleInfo{}, fmt.Errorf("%w: unreadable entry", ErrBadBundleInfo)
	}
	return ParseInfoPlist(plist)
}
