package winversion

import (
	"encoding/binary"
	"errors"
	"testing"
)

func testInfo() Info {
	return Info{Name: "Notes", ID: "dev.gofastr.notes", Version: "1.4.2", Parts: [4]uint16{1, 4, 2, 0}}
}

func TestRenderParseRoundTrip(t *testing.T) {
	got, err := ParseStrings(Render(testInfo()))
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		KeyProductVersion:  "1.4.2",
		KeyAppID:           "dev.gofastr.notes",
		"OriginalFilename": "Notes.exe",
		"Comments":         "Application ID: dev.gofastr.notes",
	} {
		if got[key] != want {
			t.Errorf("%s = %q, want %q", key, got[key], want)
		}
	}
}

// rsrcSection lays out a three-level resource tree (type 16, name 1,
// language 0x409) followed by the version blob, the way the linker and
// UpdateResource do, inside a section mapped at secVA.
func rsrcSection(blob []byte, secVA uint32) []byte {
	dir := func(entryName, target uint32) []byte {
		b := make([]byte, 16, 24)
		binary.LittleEndian.PutUint16(b[14:], 1) // one ID entry
		b = binary.LittleEndian.AppendUint32(b, entryName)
		b = binary.LittleEndian.AppendUint32(b, target)
		return b
	}
	// type dir at 0, name dir at 24, lang dir at 48, data entry at 72,
	// blob at 88.
	sec := dir(rtVersion, 0x80000000|24)
	sec = append(sec, dir(1, 0x80000000|48)...)
	sec = append(sec, dir(0x409, 72)...)
	sec = binary.LittleEndian.AppendUint32(sec, secVA+88)
	sec = binary.LittleEndian.AppendUint32(sec, uint32(len(blob)))
	sec = append(sec, make([]byte, 8)...)
	return append(sec, blob...)
}

func TestFindVersionWalksTheResourceTree(t *testing.T) {
	const secVA = 0x5000
	blob := Render(testInfo())
	found, err := findVersion(rsrcSection(blob, secVA), secVA, 0)
	if err != nil {
		t.Fatal(err)
	}
	strs, err := ParseStrings(found)
	if err != nil {
		t.Fatal(err)
	}
	if strs[KeyAppID] != "dev.gofastr.notes" {
		t.Fatalf("app ID = %q", strs[KeyAppID])
	}
}

func TestFindVersionRefusesOutOfRangeData(t *testing.T) {
	const secVA = 0x5000
	sec := rsrcSection(Render(testInfo()), secVA)
	// Point the data entry past the section.
	binary.LittleEndian.PutUint32(sec[72:], secVA+uint32(len(sec)))
	if _, err := findVersion(sec, secVA, 0); !errors.Is(err, ErrNoVersion) {
		t.Fatalf("out-of-range data entry: err = %v, want ErrNoVersion", err)
	}
	// An RVA below the section must not wrap around.
	binary.LittleEndian.PutUint32(sec[72:], secVA-4)
	if _, err := findVersion(sec, secVA, 0); !errors.Is(err, ErrNoVersion) {
		t.Fatalf("data entry below the section: err = %v, want ErrNoVersion", err)
	}
}

func TestFindVersionWithoutVersionType(t *testing.T) {
	const secVA = 0x5000
	sec := rsrcSection(Render(testInfo()), secVA)
	binary.LittleEndian.PutUint32(sec[16:], 3) // RT_ICON, not RT_VERSION
	if _, err := findVersion(sec, secVA, 0); !errors.Is(err, ErrNoVersion) {
		t.Fatalf("err = %v, want ErrNoVersion", err)
	}
}

// Every truncation and every single-byte corruption of a valid blob
// must come back as data or ErrNoVersion, never a panic.
func TestParseStringsSurvivesDamage(t *testing.T) {
	blob := Render(testInfo())
	for n := range len(blob) {
		if _, err := ParseStrings(blob[:n]); err == nil && n < 6 {
			t.Fatalf("a %d-byte blob parsed", n)
		}
	}
	for i := range len(blob) {
		for _, v := range []byte{0x00, 0xff, 0x7f} {
			damaged := append([]byte(nil), blob...)
			damaged[i] = v
			_, _ = ParseStrings(damaged)
		}
	}
}

func TestParseStringsRefusesOtherRoots(t *testing.T) {
	blob := block("NOT_VERSION_INFO", nil, 0, 0)
	if _, err := ParseStrings(blob); !errors.Is(err, ErrNoVersion) {
		t.Fatalf("err = %v, want ErrNoVersion", err)
	}
}

func FuzzParseStrings(f *testing.F) {
	f.Add(Render(testInfo()))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = ParseStrings(data)
		_, _ = findVersion(data, 0x1000, 0)
	})
}
