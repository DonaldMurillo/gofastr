package file

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"image"
	"image/jpeg"
	"image/png"
	"net/http"

	"github.com/DonaldMurillo/gofastr/framework/internal/exif"
	"github.com/DonaldMurillo/gofastr/framework/internal/jpegcost"
)

// StripMetadata removes EXIF, XMP, comments and trailers from JPEG, PNG and
// WebP originals before they are stored, keeping them upright. A supported
// image that cannot be walked fails the upload.
func StripMetadata() ProcessOption {
	return func(c *processConfig) { c.stripMetadata = true }
}

const stripJPEGQuality = 95

const stripMaxPixels int64 = 64 * 1024 * 1024

const stripMaxJPEGScans = 64

const stripMaxJPEGScanPixels = 10 * stripMaxPixels

const stripMaxProgressiveJPEGBytes int64 = 512 << 20

func checkStripPixelCap(w, h int) error {
	if w <= 0 || h <= 0 {
		return errors.New("strip: invalid image dimensions in header")
	}
	if int64(w) > stripMaxPixels/int64(h) {
		return fmt.Errorf("strip: %dx%d header exceeds the %d-pixel decode cap", w, h, stripMaxPixels)
	}
	return nil
}

func stripImageMetadata(data []byte) ([]byte, error) {
	switch http.DetectContentType(data) {
	case "image/jpeg":
		return stripJPEG(data)
	case "image/png":
		return stripPNG(data)
	case "image/webp":
		return stripWebP(data)
	}
	return nil, nil
}

const adobeAPP14Len = 12

func stripJPEG(data []byte) ([]byte, error) {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 || data[2] != 0xFF {
		return nil, errors.New("jpeg: missing SOI marker")
	}
	orient := 0
	out := make([]byte, 0, len(data))
	out = append(out, data[0], data[1])
	i, inScan, scans := 2, false, 0
	for i < len(data) {
		if inScan {
			if data[i] != 0xFF {
				out = append(out, data[i])
				i++
				continue
			}
			if i+1 >= len(data) {
				return nil, errors.New("jpeg: truncated inside scan")
			}
			n := data[i+1]
			if n == 0x00 || n == 0x01 || (n >= 0xD0 && n <= 0xD7) {
				out = append(out, data[i], data[i+1])
				i += 2
				continue
			}
			inScan = false
		}
		if i+1 >= len(data) || data[i] != 0xFF {
			return nil, fmt.Errorf("jpeg: expected marker at offset %d", i)
		}
		marker := data[i+1]
		if marker == 0x00 || marker == 0x01 || (marker >= 0xD0 && marker <= 0xD7) {
			out = append(out, data[i], data[i+1])
			i += 2
			continue
		}
		if marker == 0xFF {
			i++
			continue
		}
		if marker == 0xD9 {
			out = append(out, data[i], data[i+1])
			break
		}
		if marker == 0xD8 {
			out = append(out, data[i], data[i+1])
			i += 2
			continue
		}
		segLenAt := i + 2
		if segLenAt+2 > len(data) {
			return nil, fmt.Errorf("jpeg: segment at offset %d is truncated", i)
		}
		segLen := int(binary.BigEndian.Uint16(data[segLenAt:]))
		if segLen < 2 || segLenAt+segLen > len(data) {
			return nil, fmt.Errorf("jpeg: segment at offset %d has bad length %d", i, segLen)
		}
		payload := data[segLenAt+2 : segLenAt+segLen]
		switch {
		case marker == 0xEE && len(payload) >= adobeAPP14Len && string(payload[:5]) == "Adobe":
			out = append(out, 0xFF, 0xEE, 0x00, adobeAPP14Len+2)
			out = append(out, payload[:adobeAPP14Len]...)
		case marker >= 0xE0 && marker <= 0xEF:
			if marker == 0xE1 && orient == 0 && len(payload) >= 6 && string(payload[:6]) == "Exif\x00\x00" {
				orient = exif.ParseTIFFOrientation(payload[6:])
			}
		case marker == 0xFE:
		case marker == 0xDA:
			scans++
			out = append(out, data[i:segLenAt+segLen]...)
			i = segLenAt + segLen
			inScan = true
			continue
		default:
			out = append(out, data[i:segLenAt+segLen]...)
		}
		i = segLenAt + segLen
	}
	if orient >= 2 && orient <= 8 {
		if scans > stripMaxJPEGScans {
			return nil, fmt.Errorf("strip: JPEG carries %d scans, over the %d-scan decode cap", scans, stripMaxJPEGScans)
		}
		n, ok := jpegcost.ProgressiveCoeffBytes(out)
		if !ok {
			return nil, fmt.Errorf("strip: cannot estimate JPEG coefficient memory")
		}
		if n > stripMaxProgressiveJPEGBytes {
			return nil, fmt.Errorf("strip: progressive JPEG needs %d bytes of coefficients, over the %d-byte decode cap", n, stripMaxProgressiveJPEGBytes)
		}
		cfg, err := jpeg.DecodeConfig(bytes.NewReader(out))
		if err != nil {
			return nil, fmt.Errorf("jpeg: decoding header for orientation bake: %w", err)
		}
		if err := checkStripPixelCap(cfg.Width, cfg.Height); err != nil {
			return nil, err
		}
		if int64(scans) > stripMaxJPEGScanPixels/(int64(cfg.Width)*int64(cfg.Height)) {
			return nil, fmt.Errorf("strip: JPEG carries %d scans of a %dx%d frame, over the %d pixel-scan decode budget", scans, cfg.Width, cfg.Height, stripMaxJPEGScanPixels)
		}
		img, err := jpeg.Decode(bytes.NewReader(out))
		if err != nil {
			return nil, fmt.Errorf("jpeg: decoding for orientation: %w", err)
		}
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, applyOrientation(img, orient), &jpeg.Options{Quality: stripJPEGQuality}); err != nil {
			return nil, fmt.Errorf("jpeg: re-encoding oriented image: %w", err)
		}
		return buf.Bytes(), nil
	}
	return out, nil
}

func stripPNG(data []byte) ([]byte, error) {
	if len(data) < 8 || !bytes.Equal(data[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1A, '\n'}) {
		return nil, errors.New("png: bad signature")
	}
	orient, hasMeta, animated := 0, false, false
	iendEnd := 0
	pos := 8
	for pos+8 <= len(data) {
		length := binary.BigEndian.Uint32(data[pos : pos+4])
		typ := string(data[pos+4 : pos+8])
		if uint64(pos)+12+uint64(length) > uint64(len(data)) {
			return nil, fmt.Errorf("png: chunk %q at offset %d is truncated", typ, pos)
		}
		switch typ {
		case "tEXt", "zTXt", "iTXt", "eXIf":
			hasMeta = true
			if typ == "eXIf" && orient == 0 {
				tiff := data[pos+8 : pos+8+int(length)]
				tiff = bytes.TrimPrefix(tiff, []byte("Exif\x00\x00"))
				orient = exif.ParseTIFFOrientation(tiff)
			}
		case "acTL":
			animated = true
		case "IEND":
			iendEnd = pos + 12 + int(length)
			pos = len(data)
			continue
		}
		pos += 12 + int(length)
	}
	if !hasMeta {
		if iendEnd > 0 && iendEnd < len(data) {
			return data[:iendEnd], nil
		}
		return data, nil
	}
	if animated {
		return spliceAPNG(data, orient), nil
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("png: decoding header for metadata strip: %w", err)
	}
	if err := checkStripPixelCap(cfg.Width, cfg.Height); err != nil {
		return nil, err
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("png: decoding for metadata strip: %w", err)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, applyOrientation(img, orient)); err != nil {
		return nil, fmt.Errorf("png: re-encoding: %w", err)
	}
	return buf.Bytes(), nil
}

func spliceAPNG(data []byte, orient int) []byte {
	out := append(make([]byte, 0, len(data)), data[:8]...)
	keptOrient := false
	for pos := 8; pos+8 <= len(data); {
		length := int(binary.BigEndian.Uint32(data[pos : pos+4]))
		typ := string(data[pos+4 : pos+8])
		end := pos + 12 + length
		switch typ {
		case "tEXt", "zTXt", "iTXt":
		case "eXIf":
			if orient >= 2 && orient <= 8 && !keptOrient {
				out = appendPNGChunk(out, "eXIf", minimalOrientationTIFF(orient))
				keptOrient = true
			}
		default:
			out = append(out, data[pos:end]...)
		}
		if typ == "IEND" {
			break
		}
		pos = end
	}
	return out
}

func appendPNGChunk(out []byte, typ string, payload []byte) []byte {
	out = binary.BigEndian.AppendUint32(out, uint32(len(payload)))
	out = append(out, typ...)
	out = append(out, payload...)
	crc := crc32.NewIEEE()
	crc.Write([]byte(typ))
	crc.Write(payload)
	return binary.BigEndian.AppendUint32(out, crc.Sum32())
}

type webpChunk struct {
	typ     string
	payload []byte
}

func stripWebP(data []byte) ([]byte, error) {
	if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		return nil, errors.New("webp: bad RIFF header")
	}
	riffEnd := 8 + int(uint32(binary.LittleEndian.Uint32(data[4:8])))
	if riffEnd <= 12 {
		return nil, fmt.Errorf("webp: RIFF size %d leaves no chunks", riffEnd)
	}
	if riffEnd > len(data) {
		return nil, fmt.Errorf("webp: RIFF size %d exceeds the file", riffEnd)
	}
	var kept []webpChunk
	orient, changed := 0, false
	pos := 12
	for pos < riffEnd {
		if pos+8 > riffEnd {
			return nil, fmt.Errorf("webp: chunk header at offset %d is truncated", pos)
		}
		ct := string(data[pos : pos+4])
		cl := int(uint32(binary.LittleEndian.Uint32(data[pos+4 : pos+8])))
		if uint64(pos)+8+uint64(cl) > uint64(riffEnd) {
			return nil, fmt.Errorf("webp: chunk %q at offset %d is truncated", ct, pos)
		}
		payload := data[pos+8 : pos+8+cl]
		switch ct {
		case "EXIF":
			changed = true
			if orient == 0 {
				tiff := bytes.TrimPrefix(payload, []byte("Exif\x00\x00"))
				orient = exif.ParseTIFFOrientation(tiff)
			}
		case "XMP ":
			changed = true
		default:
			kept = append(kept, webpChunk{typ: ct, payload: payload})
		}
		pos += 8 + cl
		if cl%2 == 1 {
			pos++
		}
	}
	if !changed {
		return data[:riffEnd], nil
	}
	keepEXIF := false
	if orient >= 2 && orient <= 8 {
		kept = append(kept, webpChunk{typ: "EXIF", payload: minimalOrientationTIFF(orient)})
		keepEXIF = true
	}
	fixVP8XFlags(kept, keepEXIF)

	var buf bytes.Buffer
	buf.WriteString("RIFF")
	buf.Write([]byte{0, 0, 0, 0})
	buf.WriteString("WEBP")
	for _, c := range kept {
		buf.WriteString(c.typ)
		var lb [4]byte
		binary.LittleEndian.PutUint32(lb[:], uint32(len(c.payload)))
		buf.Write(lb[:])
		buf.Write(c.payload)
		if len(c.payload)%2 == 1 {
			buf.WriteByte(0)
		}
	}
	out := buf.Bytes()
	binary.LittleEndian.PutUint32(out[4:8], uint32(len(out)-8))
	return out, nil
}

const (
	webpVP8XEXIFFlag = 0x08
	webpVP8XXMPFlag  = 0x04
)

func fixVP8XFlags(chunks []webpChunk, exifKept bool) {
	for _, c := range chunks {
		if c.typ != "VP8X" || len(c.payload) < 1 {
			continue
		}
		c.payload[0] &^= webpVP8XXMPFlag
		if exifKept {
			c.payload[0] |= webpVP8XEXIFFlag
		} else {
			c.payload[0] &^= webpVP8XEXIFFlag
		}
		return
	}
}

func minimalOrientationTIFF(orientation int) []byte {
	return []byte{
		'I', 'I', 0x2A, 0x00,
		0x08, 0x00, 0x00, 0x00,
		0x01, 0x00,
		0x12, 0x01,
		0x03, 0x00,
		0x01, 0x00, 0x00, 0x00,
		byte(orientation), 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
	}
}

func applyOrientation(img image.Image, orient int) image.Image {
	switch orient {
	case 2:
		return flopImage(img)
	case 3:
		return rotate180(img)
	case 4:
		return flipImage(img)
	case 5:
		return flopImage(rotate90(img))
	case 6:
		return rotate90(img)
	case 7:
		return flopImage(rotate270(img))
	case 8:
		return rotate270(img)
	}
	return img
}

func rotate90(img image.Image) *image.RGBA {
	b := img.Bounds()
	sw, sh := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, sh, sw))
	for y := range sh {
		for x := range sw {
			dst.Set(sh-1-y, x, img.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

func rotate180(img image.Image) *image.RGBA {
	b := img.Bounds()
	sw, sh := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, sw, sh))
	for y := range sh {
		for x := range sw {
			dst.Set(sw-1-x, sh-1-y, img.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

func rotate270(img image.Image) *image.RGBA {
	b := img.Bounds()
	sw, sh := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, sh, sw))
	for y := range sh {
		for x := range sw {
			dst.Set(y, sw-1-x, img.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

func flipImage(img image.Image) *image.RGBA {
	b := img.Bounds()
	sw, sh := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, sw, sh))
	for y := range sh {
		for x := range sw {
			dst.Set(x, sh-1-y, img.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

func flopImage(img image.Image) *image.RGBA {
	b := img.Bounds()
	sw, sh := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, sw, sh))
	for y := range sh {
		for x := range sw {
			dst.Set(sw-1-x, y, img.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}
