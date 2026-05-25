package image

import "bytes"

// Format identifies an image container format.
type Format int

const (
	FormatUnknown Format = iota
	FormatJPEG
	FormatPNG
	FormatGIF
	FormatBMP
	FormatTIFF
	FormatWebP
)

func (f Format) String() string {
	switch f {
	case FormatJPEG:
		return "jpeg"
	case FormatPNG:
		return "png"
	case FormatGIF:
		return "gif"
	case FormatBMP:
		return "bmp"
	case FormatTIFF:
		return "tiff"
	case FormatWebP:
		return "webp"
	}
	return "unknown"
}

// MIME returns the canonical Content-Type for the format, or
// application/octet-stream for FormatUnknown.
func (f Format) MIME() string {
	switch f {
	case FormatJPEG:
		return "image/jpeg"
	case FormatPNG:
		return "image/png"
	case FormatGIF:
		return "image/gif"
	case FormatBMP:
		return "image/bmp"
	case FormatTIFF:
		return "image/tiff"
	case FormatWebP:
		return "image/webp"
	}
	return "application/octet-stream"
}

// Sniff returns the Format detected from the first bytes of data.
func Sniff(data []byte) Format {
	if len(data) < 12 {
		return FormatUnknown
	}
	switch {
	case bytes.HasPrefix(data, []byte{0xFF, 0xD8, 0xFF}):
		return FormatJPEG
	case bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		return FormatPNG
	case bytes.HasPrefix(data, []byte("GIF87a")) || bytes.HasPrefix(data, []byte("GIF89a")):
		return FormatGIF
	case bytes.HasPrefix(data, []byte("BM")):
		return FormatBMP
	case bytes.HasPrefix(data, []byte{0x49, 0x49, 0x2A, 0x00}),
		bytes.HasPrefix(data, []byte{0x4D, 0x4D, 0x00, 0x2A}):
		return FormatTIFF
	case bytes.HasPrefix(data, []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")):
		return FormatWebP
	}
	return FormatUnknown
}
