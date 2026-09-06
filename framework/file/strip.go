package file

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"net/http"
)

// StripMetadata returns a [ProcessOption] that removes privacy-bearing
// metadata from stored image originals: EXIF (GPS coordinates, camera
// serials, embedded thumbnails), XMP, and equivalent segments — JPEG
// APPn and COM segments, PNG tEXt/zTXt/iTXt/eXIf chunks, and WebP EXIF
// and XMP chunks. A non-trivial EXIF orientation is applied first, so
// the stripped original still displays upright: JPEG and PNG re-encode
// with the orientation baked into the pixels, WebP (no encoder at this
// layer) keeps an orientation-only EXIF chunk instead.
//
// It is OFF by default: originals are stored byte-for-byte, metadata
// included. That default is deliberate and pinned by
// TestProcessFileField_KeepsEXIFByDefault; see the uploads doc page.
//
// Stripping is gated on the sniffed content, never the filename, and
// runs before [WithImageDeriver], so renditions are derived from the
// stripped bytes too. A JPEG, PNG, or WebP whose structure cannot be
// walked fails the upload rather than being stored unstripped
// (fail-closed: the caller asked that this object not keep its
// metadata). Other formats pass through unchanged.
func StripMetadata() ProcessOption {
	return func(c *processConfig) { c.stripMetadata = true }
}

// stripJPEGQuality is the re-encode quality used when an EXIF
// orientation has to be baked into a JPEG's pixels. Stripping without a
// rotation is a pure segment splice and never re-encodes; only the
// rotate path pays this, so it is set high enough that the visible cost
// of one re-encode generation stays small.
const stripJPEGQuality = 95

// stripImageMetadata strips metadata segments from data when it is a
// JPEG, PNG, or WebP, returning the replacement bytes. A nil result
// with a nil error means "not a supported image type / nothing changed":
// the caller keeps the original bytes. Everything else is an error and
// the upload must be refused, the caller opted into stripping, so
// storing the bytes unstripped is not an outcome.
func stripImageMetadata(data []byte) ([]byte, error) {
	// Gate on content, never the filename: a file named photo.jpg whose
	// bytes are not a JPEG is not this function's business.
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

// stripJPEG removes every APPn (EXIF, XMP, ICC, Photoshop IRB, …) and
// COM segment from a JPEG and drops any post-EOI trailer, without
// re-encoding: the scan data is copied verbatim, so nothing that did
// not carry metadata is re-compressed. If the EXIF orientation tag is
// 2..8 the spliced stream is decoded, rotated into the upright frame,
// and re-encoded once (quality stripJPEGQuality), because dropping the
// tag without baking it would leave the photo sideways.
//
// The walk is strict: any structural violation (marker expected but
// absent, segment length past the end of the file) is an error rather
// than a best-effort copy, since a partial splice could silently keep
// the segment the caller asked to lose.
func stripJPEG(data []byte) ([]byte, error) {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 || data[2] != 0xFF {
		return nil, errors.New("jpeg: missing SOI marker")
	}
	orient := 0
	out := make([]byte, 0, len(data))
	out = append(out, data[0], data[1])
	i, inScan := 2, false
	for i < len(data) {
		if inScan {
			// Entropy-coded scan data: copy bytes until the next
			// marker. 0xFF is legal here only as byte stuffing
			// (0xFF00), an RST marker, or the marker that ends the
			// scan (progressive files repeat this per scan).
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
			// A real marker ends the scan; fall through to marker
			// handling at i.
			inScan = false
		}
		if i+1 >= len(data) || data[i] != 0xFF {
			return nil, fmt.Errorf("jpeg: expected marker at offset %d", i)
		}
		marker := data[i+1]
		// Stand-alone markers carry no length field.
		if marker == 0x00 || marker == 0x01 || (marker >= 0xD0 && marker <= 0xD7) {
			out = append(out, data[i], data[i+1])
			i += 2
			continue
		}
		if marker == 0xFF {
			// Fill byte before a marker: skip one 0xFF and stay put.
			i++
			continue
		}
		if marker == 0xD9 { // EOI: done, any trailer after it is dropped
			out = append(out, data[i], data[i+1])
			break
		}
		if marker == 0xD8 { // embedded SOI (shouldn't happen at top level)
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
		case marker >= 0xE0 && marker <= 0xEF: // APPn: metadata, dropped
			if marker == 0xE1 && orient == 0 && len(payload) >= 6 && string(payload[:6]) == "Exif\x00\x00" {
				orient = parseTIFFOrientation(payload[6:])
			}
		case marker == 0xFE: // COM: comment, dropped
		case marker == 0xDA: // SOS: header copied, scan data follows
			out = append(out, data[i:segLenAt+segLen]...)
			i = segLenAt + segLen
			inScan = true
			continue
		default: // DQT, DHT, SOF, DRI, …: image structure, copied
			out = append(out, data[i:segLenAt+segLen]...)
		}
		i = segLenAt + segLen
	}
	if orient >= 2 && orient <= 8 {
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

// stripPNG re-encodes a PNG when it carries any metadata chunk
// (tEXt/zTXt/iTXt/eXIf), which drops them all: the stdlib encoder
// writes only the chunks it understands, no text or EXIF survives, and
// PNG is lossless so the re-encode costs nothing but CPU. A PNG with no
// metadata chunk is returned unchanged, byte-for-byte. An eXIf
// orientation of 2..8 is baked into the pixels before re-encoding.
func stripPNG(data []byte) ([]byte, error) {
	if len(data) < 8 || !bytes.Equal(data[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1A, '\n'}) {
		return nil, errors.New("png: bad signature")
	}
	orient, hasMeta := 0, false
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
				// Some writers prefix the TIFF stream with the JPEG
				// APP1 identifier; tolerate both shapes.
				tiff = bytes.TrimPrefix(tiff, []byte("Exif\x00\x00"))
				orient = parseTIFFOrientation(tiff)
			}
		case "IEND":
			// Stop like the decoder does; bytes after IEND only
			// survive when there is nothing to strip.
			pos = len(data)
			continue
		}
		pos += 12 + int(length)
	}
	if !hasMeta {
		return data, nil
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

// webpChunk is one chunk of a WebP's RIFF container.
type webpChunk struct {
	typ     string
	payload []byte
}

// stripWebP drops the EXIF and XMP chunks from a WebP container without
// touching the image data: the remaining chunks (VP8X, VP8L/VP8, ALPH,
// ICCP, ANIM, …) are copied verbatim and the RIFF sizes are rebuilt.
// There is no WebP encoder at this layer (it lives in framework/image,
// one banned edge away), so an EXIF orientation of 2..8 cannot be baked
// into pixels; instead the EXIF chunk is rewritten as an orientation-
// only payload, which keeps the display upright while dropping
// everything else the original EXIF carried. The VP8X flags are kept
// consistent with the chunks that remain.
func stripWebP(data []byte) ([]byte, error) {
	if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		return nil, errors.New("webp: bad RIFF header")
	}
	riffEnd := 8 + int(uint32(binary.LittleEndian.Uint32(data[4:8])))
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
				// libwebp writes the TIFF stream bare; some writers
				// keep the JPEG APP1 identifier. Accept both.
				tiff := bytes.TrimPrefix(payload, []byte("Exif\x00\x00"))
				orient = parseTIFFOrientation(tiff)
			}
		case "XMP ":
			changed = true
		default:
			kept = append(kept, webpChunk{typ: ct, payload: payload})
		}
		pos += 8 + cl
		if cl%2 == 1 {
			pos++ // odd payloads are padded; the pad byte is not in cl
		}
	}
	if !changed {
		return data, nil
	}
	keepEXIF := false
	if orient >= 2 && orient <= 8 {
		kept = append(kept, webpChunk{typ: "EXIF", payload: minimalOrientationTIFF(orient)})
		keepEXIF = true
	}
	fixVP8XFlags(kept, keepEXIF)

	var buf bytes.Buffer
	buf.WriteString("RIFF")
	buf.Write([]byte{0, 0, 0, 0}) // size patched below
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

// fixVP8XFlags makes the VP8X feature flags agree with the chunks that
// are actually present after a strip: a container that still advertises
// a dropped chunk is malformed for strict readers.
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

// minimalOrientationTIFF builds a TIFF stream whose IFD0 holds exactly
// one entry, the orientation tag. Byte layout mirrors the test fixture
// in framework/image (insertExifAPP1) and round-trips through
// parseTIFFOrientation in both packages.
func minimalOrientationTIFF(orientation int) []byte {
	return []byte{
		'I', 'I', 0x2A, 0x00, // little-endian TIFF magic
		0x08, 0x00, 0x00, 0x00, // IFD0 at offset 8
		0x01, 0x00, // one entry
		0x12, 0x01, // tag 0x0112 (orientation)
		0x03, 0x00, // type SHORT
		0x01, 0x00, 0x00, 0x00, // count 1
		byte(orientation), 0x00, 0x00, 0x00, // value in the 4-byte slot
		0x00, 0x00, 0x00, 0x00, // no next IFD
	}
}

// parseTIFFOrientation reads a TIFF stream and returns the orientation
// tag (0x0112) from IFD0, or 0 if absent. It is a leaf-local twin of
// framework/image's parseTIFFOrientation: this package cannot import
// that one (the layering rule that keeps image codecs out of every CRUD
// binary), and core/upload is not an EXIF home. The two are pinned
// against each other by TestStripOrientationBakeMatchesPipeline.
func parseTIFFOrientation(tiff []byte) int {
	if len(tiff) < 8 {
		return 0
	}
	var order binary.ByteOrder
	switch {
	case tiff[0] == 'I' && tiff[1] == 'I' && tiff[2] == 0x2A && tiff[3] == 0x00:
		order = binary.LittleEndian
	case tiff[0] == 'M' && tiff[1] == 'M' && tiff[2] == 0x00 && tiff[3] == 0x2A:
		order = binary.BigEndian
	default:
		return 0
	}
	ifd0Offset := int(order.Uint32(tiff[4:]))
	if ifd0Offset < 8 || ifd0Offset+2 > len(tiff) {
		return 0
	}
	numEntries := int(order.Uint16(tiff[ifd0Offset:]))
	entries := tiff[ifd0Offset+2:]
	if numEntries*12 > len(entries) {
		return 0
	}
	for n := 0; n < numEntries; n++ {
		e := entries[n*12 : n*12+12]
		tag := order.Uint16(e[0:2])
		if tag != 0x0112 {
			continue
		}
		typ := order.Uint16(e[2:4])
		count := order.Uint32(e[4:8])
		if typ != 3 /* SHORT */ || count != 1 {
			return 0
		}
		v := int(order.Uint16(e[8:10]))
		if v < 1 || v > 8 {
			return 0
		}
		return v
	}
	return 0
}

// applyOrientation returns img with the EXIF orientation tag baked into
// its pixels. The case mapping is the EXIF standard one and the pixel
// transforms are twins of framework/image's Rotate/Flip/Flop (pinned by
// TestStripOrientationBakeMatchesPipeline); they are re-implemented
// here because this package must not import the image pipeline.
//
//	1: identity                 5: transpose (rotate 90 then flop)
//	2: flop                     6: rotate 90 CW
//	3: rotate 180               7: transverse (rotate 270 then flop)
//	4: flip                     8: rotate 270 CW
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

// rotate90 returns img rotated 90 degrees clockwise.
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

// rotate180 returns img rotated 180 degrees.
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

// rotate270 returns img rotated 270 degrees clockwise (90 CCW).
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

// flipImage mirrors img vertically (top↔bottom).
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

// flopImage mirrors img horizontally (left↔right).
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
