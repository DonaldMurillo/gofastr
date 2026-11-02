package winversion

import (
	"encoding/binary"
	"unicode/utf16"
)

// String keys the desktop build writes into the resource's string
// table. KeyAppID is GoFastr's own: a build without it predates the
// Windows updater, and the updater treats it as unbundled.
const (
	KeyProductVersion = "ProductVersion"
	KeyAppID          = "GoFastrAppID"
)

// Info is the identity one build embeds.
type Info struct {
	Name    string // product name; the executable is Name + ".exe"
	ID      string // reverse-DNS application ID
	Version string // the --version string, verbatim
	// Parts is Version's numeric form for the fixed file version
	// fields: up to four components, each 0-65535.
	Parts [4]uint16
}

// Render returns the RT_VERSION resource bytes for info.
func Render(info Info) []byte {
	stringsTable := block("040904B0", nil, 0, 1,
		stringEntry("CompanyName", "GoFastr"),
		stringEntry("FileDescription", info.Name),
		stringEntry("FileVersion", info.Version),
		stringEntry("InternalName", info.Name),
		stringEntry("OriginalFilename", info.Name+".exe"),
		stringEntry("ProductName", info.Name),
		stringEntry(KeyProductVersion, info.Version),
		stringEntry("Comments", "Application ID: "+info.ID),
		stringEntry(KeyAppID, info.ID),
	)
	stringFileInfo := block("StringFileInfo", nil, 0, 1, stringsTable)
	translation := []byte{0x09, 0x04, 0xB0, 0x04} // LANG_ENGLISH/US, Unicode
	translationEntry := block("Translation", translation, uint16(len(translation)), 0)
	varFileInfo := block("VarFileInfo", nil, 0, 1, translationEntry)

	fileVersionMS := uint32(info.Parts[0])<<16 | uint32(info.Parts[1])
	fileVersionLS := uint32(info.Parts[2])<<16 | uint32(info.Parts[3])
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
	return block("VS_VERSION_INFO", fixed, uint16(len(fixed)), 0, stringFileInfo, varFileInfo)
}

// stringEntry is one String block: a UTF-16 key and a NUL-terminated
// UTF-16 value, its length counted in WORDs.
func stringEntry(key, value string) []byte {
	encoded := utf16.Encode([]rune(value))
	encoded = append(encoded, 0)
	data := make([]byte, 0, len(encoded)*2)
	for _, unit := range encoded {
		data = binary.LittleEndian.AppendUint16(data, unit)
	}
	return block(key, data, uint16(len(encoded)), 1)
}

// block renders one VERSIONINFO node: wLength, wValueLength, wType,
// the NUL-terminated key, padding, the value, padding, the children.
func block(key string, value []byte, valueLength, valueType uint16, children ...[]byte) []byte {
	b := make([]byte, 6, 6+len(key)*2+len(value))
	for _, unit := range utf16.Encode([]rune(key + "\x00")) {
		b = binary.LittleEndian.AppendUint16(b, unit)
	}
	align(&b)
	b = append(b, value...)
	align(&b)
	for _, child := range children {
		b = append(b, child...)
		align(&b)
	}
	if len(b) > 0xFFFF {
		panic("winversion: version resource block exceeds 65535 bytes")
	}
	binary.LittleEndian.PutUint16(b[0:2], uint16(len(b)))
	binary.LittleEndian.PutUint16(b[2:4], valueLength)
	binary.LittleEndian.PutUint16(b[4:6], valueType)
	return b
}

func align(b *[]byte) {
	for len(*b)%4 != 0 {
		*b = append(*b, 0)
	}
}
