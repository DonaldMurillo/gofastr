package winversion

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"unicode/utf16"
)

// ErrNoVersion is the sentinel for an executable the reader cannot
// take a version resource from: not a PE file, no resource section, no
// RT_VERSION entry, or a resource that does not parse. Its wrapped
// messages name no path.
var ErrNoVersion = errors.New("executable has no readable version resource")

const (
	rtVersion = 16
	// maxResourceSection caps the resource section read. A desktop
	// build's is a few hundred KiB of icons and the version block.
	maxResourceSection = 16 << 20
	// maxDepth bounds the VERSIONINFO tree walk: VS_VERSION_INFO,
	// StringFileInfo, the string table, the strings.
	maxDepth = 4
)

// Strings is the string table of a version resource, keyed by the
// String block's key (ProductVersion, GoFastrAppID, ...). A key that
// appears twice keeps its first value.
type Strings map[string]string

// ReadFile reads the version resource strings from the PE file at path.
//
// It reads the headers itself rather than through debug/pe: the
// resource-update API that embeds the version rewrites the file and
// leaves the COFF symbol-table pointer aimed past the end, which
// debug/pe refuses. Only the section table is needed here.
func ReadFile(path string) (Strings, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return read(f)
}

// read parses the DOS stub, the PE signature, the COFF header, the
// optional header's resource data directory and the section table,
// then the section that holds the resource directory.
func read(r io.ReaderAt) (Strings, error) {
	bad := func(what string) (Strings, error) {
		return nil, fmt.Errorf("%w: %s", ErrNoVersion, what)
	}
	var dos [64]byte
	if _, err := r.ReadAt(dos[:], 0); err != nil || dos[0] != 'M' || dos[1] != 'Z' {
		return bad("not a PE file")
	}
	peOff := int64(binary.LittleEndian.Uint32(dos[0x3c:]))
	var hdr [24]byte // the PE signature, then IMAGE_FILE_HEADER
	if _, err := r.ReadAt(hdr[:], peOff); err != nil || string(hdr[:4]) != "PE\x00\x00" {
		return bad("not a PE file")
	}
	nsec := int(binary.LittleEndian.Uint16(hdr[6:]))
	optSize := int(binary.LittleEndian.Uint16(hdr[20:]))
	if nsec == 0 || nsec > 96 || optSize < 2 || optSize > 4096 {
		return bad("PE header out of range")
	}
	opt := make([]byte, optSize)
	if _, err := r.ReadAt(opt, peOff+24); err != nil {
		return bad("truncated optional header")
	}
	// Data directories start at 96 (PE32) or 112 (PE32+); the count
	// sits just before them.
	var ddOff int
	switch binary.LittleEndian.Uint16(opt) {
	case 0x10b:
		ddOff = 96
	case 0x20b:
		ddOff = 112
	default:
		return bad("unknown optional header")
	}
	const resourceIndex = 2
	if ddOff+8*(resourceIndex+1) > optSize || binary.LittleEndian.Uint32(opt[ddOff-4:]) <= resourceIndex {
		return bad("no resource directory")
	}
	dirVA := binary.LittleEndian.Uint32(opt[ddOff+8*resourceIndex:])
	if dirVA == 0 {
		return bad("no resource directory")
	}
	secs := make([]byte, 40*nsec)
	if _, err := r.ReadAt(secs, peOff+24+int64(optSize)); err != nil {
		return bad("truncated section table")
	}
	for i := range nsec {
		h := secs[i*40:]
		vsize := binary.LittleEndian.Uint32(h[8:])
		va := binary.LittleEndian.Uint32(h[12:])
		rawSize := binary.LittleEndian.Uint32(h[16:])
		rawPtr := binary.LittleEndian.Uint32(h[20:])
		if dirVA < va || dirVA-va >= max(vsize, rawSize) {
			continue
		}
		if rawSize > maxResourceSection {
			return bad("resource section too large")
		}
		data := make([]byte, rawSize)
		if _, err := r.ReadAt(data, int64(rawPtr)); err != nil && !errors.Is(err, io.EOF) {
			return bad("resource section unreadable")
		}
		blob, err := findVersion(data, va, dirVA-va)
		if err != nil {
			return nil, err
		}
		return ParseStrings(blob)
	}
	return bad("resource directory is outside every section")
}

// findVersion walks the resource tree rooted at sec[root:] (type,
// then name, then language) to the first RT_VERSION data entry and
// returns its bytes. secVA is the section's RVA: data entries carry
// RVAs, not offsets.
func findVersion(sec []byte, secVA, root uint32) ([]byte, error) {
	off, isDir, ok := dirEntry(sec, root, root, rtVersion, true)
	if !ok || !isDir {
		return nil, fmt.Errorf("%w: no RT_VERSION entry", ErrNoVersion)
	}
	// Name level, then language level: take the first entry of each.
	for range 2 {
		next, sub, ok := dirEntry(sec, root, off, 0, false)
		if !ok {
			return nil, fmt.Errorf("%w: RT_VERSION entry is malformed", ErrNoVersion)
		}
		off = next
		if !sub {
			break
		}
	}
	// off now points at an IMAGE_RESOURCE_DATA_ENTRY.
	if uint64(off)+16 > uint64(len(sec)) {
		return nil, fmt.Errorf("%w: RT_VERSION data entry is out of range", ErrNoVersion)
	}
	rva := binary.LittleEndian.Uint32(sec[off:])
	size := binary.LittleEndian.Uint32(sec[off+4:])
	if rva < secVA {
		return nil, fmt.Errorf("%w: RT_VERSION data is out of range", ErrNoVersion)
	}
	start := uint64(rva - secVA)
	if start+uint64(size) > uint64(len(sec)) || size < 6 {
		return nil, fmt.Errorf("%w: RT_VERSION data is out of range", ErrNoVersion)
	}
	return sec[start : start+uint64(size)], nil
}

// dirEntry reads the IMAGE_RESOURCE_DIRECTORY at sec[off:] and returns
// the target of the entry with the given ID (byID) or of its first
// entry. Offsets in the tree are relative to root.
func dirEntry(sec []byte, root, off uint32, id uint32, byID bool) (target uint32, isDir, ok bool) {
	if uint64(off)+16 > uint64(len(sec)) {
		return 0, false, false
	}
	named := uint32(binary.LittleEndian.Uint16(sec[off+12:]))
	ids := uint32(binary.LittleEndian.Uint16(sec[off+14:]))
	total := named + ids
	if uint64(off)+16+uint64(total)*8 > uint64(len(sec)) {
		return 0, false, false
	}
	for i := range total {
		e := off + 16 + i*8
		name := binary.LittleEndian.Uint32(sec[e:])
		data := binary.LittleEndian.Uint32(sec[e+4:])
		if byID && (name&0x80000000 != 0 || name != id) {
			continue
		}
		t := uint64(root) + uint64(data&0x7fffffff)
		if t >= uint64(len(sec)) {
			return 0, false, false
		}
		return uint32(t), data&0x80000000 != 0, true
	}
	return 0, false, false
}

// ParseStrings reads the StringFileInfo string table out of a
// VS_VERSIONINFO blob.
func ParseStrings(blob []byte) (Strings, error) {
	root, err := parseNode(blob, 0, len(blob), 0)
	if err != nil {
		return nil, err
	}
	if root.key != "VS_VERSION_INFO" {
		return nil, fmt.Errorf("%w: root block is not VS_VERSION_INFO", ErrNoVersion)
	}
	out := Strings{}
	for _, sfi := range root.children {
		if sfi.key != "StringFileInfo" {
			continue
		}
		for _, table := range sfi.children {
			for _, s := range table.children {
				if _, seen := out[s.key]; !seen {
					out[s.key] = s.text
				}
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: no string table", ErrNoVersion)
	}
	return out, nil
}

type node struct {
	key      string
	text     string // a text value (wType 1), up to its NUL
	children []node
}

// parseNode parses the block at data[off:] that must end by limit.
func parseNode(data []byte, off, limit, depth int) (node, error) {
	bad := func(what string) (node, error) {
		return node{}, fmt.Errorf("%w: %s", ErrNoVersion, what)
	}
	if depth > maxDepth {
		return bad("version tree too deep")
	}
	if off+6 > limit {
		return bad("truncated block header")
	}
	length := int(binary.LittleEndian.Uint16(data[off:]))
	valueLength := int(binary.LittleEndian.Uint16(data[off+2:]))
	valueType := binary.LittleEndian.Uint16(data[off+4:])
	end := off + length
	if length < 6 || end > limit {
		return bad("block length out of range")
	}
	key, p, ok := readUTF16Z(data, off+6, end)
	if !ok {
		return bad("unterminated block key")
	}
	p = alignUp(p)
	n := node{key: key}
	if valueLength > 0 {
		size := valueLength
		if valueType == 1 {
			size = valueLength * 2 // text values count WORDs
		}
		if p > end {
			return bad("value out of range")
		}
		if valueType == 1 {
			text, _, _ := readUTF16Z(data, p, min(p+size, end))
			n.text = text
		}
		p = alignUp(min(p+size, end))
	}
	for p+6 <= end {
		child, err := parseNode(data, p, end, depth+1)
		if err != nil {
			return node{}, err
		}
		n.children = append(n.children, child)
		next := alignUp(p + int(binary.LittleEndian.Uint16(data[p:])))
		if next <= p {
			return bad("block does not advance")
		}
		p = next
	}
	return n, nil
}

// readUTF16Z decodes UTF-16LE from data[p:end] up to a NUL and returns
// the offset just past the NUL. Without a NUL it decodes to end and
// reports ok=false.
func readUTF16Z(data []byte, p, end int) (string, int, bool) {
	var units []uint16
	for p+2 <= end {
		u := binary.LittleEndian.Uint16(data[p:])
		p += 2
		if u == 0 {
			return string(utf16.Decode(units)), p, true
		}
		units = append(units, u)
	}
	return string(utf16.Decode(units)), p, false
}

func alignUp(p int) int { return (p + 3) &^ 3 }
