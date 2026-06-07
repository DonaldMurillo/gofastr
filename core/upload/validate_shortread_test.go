package upload

import (
	"errors"
	"io"
	"testing"
)

// shortReadSeeker wraps a byte slice but caps every Read at maxChunk bytes,
// simulating a chunked/non-seekable stream that dribbles the magic bytes
// across multiple short Reads. Seek is supported so ValidateMIME can reset.
type shortReadSeeker struct {
	data     []byte
	pos      int
	maxChunk int
}

func (s *shortReadSeeker) Read(p []byte) (int, error) {
	if s.pos >= len(s.data) {
		return 0, io.EOF
	}
	n := len(p)
	if n > s.maxChunk {
		n = s.maxChunk
	}
	if rem := len(s.data) - s.pos; n > rem {
		n = rem
	}
	copy(p, s.data[s.pos:s.pos+n])
	s.pos += n
	return n, nil
}

func (s *shortReadSeeker) Seek(offset int64, whence int) (int64, error) {
	if whence != io.SeekStart {
		return 0, errors.New("unsupported whence")
	}
	s.pos = int(offset)
	return offset, nil
}

// TestMIME_DetectedAcrossShortReads verifies that ValidateMIME fills its
// 512-byte sniff window even when the reader returns the magic bytes in
// small chunks. A single file.Read() can short-read, so the detector must
// keep reading until the window is full (or EOF).
func TestMIME_DetectedAcrossShortReads(t *testing.T) {
	// PNG signature followed by enough padding that detection of the type
	// requires bytes beyond the first short chunk.
	pngSig := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	body := make([]byte, 600)
	copy(body, pngSig)

	// maxChunk=1 forces one byte per Read, so a single Read sees only the
	// first signature byte (0x89) which DetectContentType classifies as
	// "application/octet-stream", not image/png.
	r := &shortReadSeeker{data: body, maxChunk: 1}

	err := ValidateMIME(r, []string{"image/png"})
	if err != nil {
		t.Errorf("PNG not detected when magic bytes arrive across short reads: %v", err)
	}
}
