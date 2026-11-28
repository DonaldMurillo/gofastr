package desktopcmd

import (
	"bytes"
	"encoding/binary"
	"fmt"
	stdimage "image"
	"image/draw"
	"os"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	fwimage "github.com/DonaldMurillo/gofastr/framework/image"
)

func loadIconSource(path string) ([]byte, string) {
	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			cli.Fail("Read --icon: %v", err)
			cli.OsExit(1)
		}
		return b, path
	}
	img, err := fwimage.NewGradient(1024, 1024, "#0E7C86", "#0E7C86")
	if err != nil {
		cli.Fail("Generate default icon: %v", err)
		cli.OsExit(1)
	}
	b, err := img.PNG().Bytes()
	if err != nil {
		cli.Fail("Encode default icon: %v", err)
		cli.OsExit(1)
	}
	return b, "generated flat square (framework/image)"
}

type icnsEntry struct {
	Type string
	Size int
}

var icnsEntries = []icnsEntry{
	{"ic07", 128},
	{"ic08", 256},
	{"ic09", 512},
	{"ic10", 1024},
	{"ic11", 32},
	{"ic12", 64},
	{"ic13", 256},
	{"ic14", 512},
}

func buildICNS(png []byte) ([]byte, error) {
	src, err := fwimage.DecodeBytes(png)
	if err != nil {
		return nil, fmt.Errorf("decode icon: %w", err)
	}
	square := squareCropStd(src.GoImage())

	var entries bytes.Buffer
	for _, e := range icnsEntries {
		resized := fwimage.FromImage(square, fwimage.FormatPNG).Resize(e.Size, e.Size)
		payload, err := resized.PNG().Bytes()
		if err != nil {
			return nil, fmt.Errorf("encode %s (%dpx): %w", e.Type, e.Size, err)
		}
		entries.WriteString(e.Type)
		var lenBuf [4]byte
		binary.BigEndian.PutUint32(lenBuf[:], uint32(len(payload)+8))
		entries.Write(lenBuf[:])
		entries.Write(payload)
	}

	body := entries.Bytes()
	out := bytes.NewBuffer(make([]byte, 0, len(body)+8))
	out.WriteString("icns")
	var total [4]byte
	binary.BigEndian.PutUint32(total[:], uint32(len(body)+8))
	out.Write(total[:])
	out.Write(body)
	return out.Bytes(), nil
}

func buildWindowsICO(pngBytes []byte) ([]byte, error) {
	src, err := fwimage.DecodeBytes(pngBytes)
	if err != nil {
		return nil, fmt.Errorf("decode icon: %w", err)
	}
	square := squareCropStd(src.GoImage())
	const count = 4
	const headerBytes = 6 + count*16
	out := make([]byte, headerBytes)
	binary.LittleEndian.PutUint16(out[2:4], 1) // ICONDIR type
	binary.LittleEndian.PutUint16(out[4:6], count)
	offset := uint32(headerBytes)
	for i, size := range []int{16, 32, 48, 256} {
		resized := fwimage.FromImage(square, fwimage.FormatPNG).Resize(size, size)
		payload, err := resized.PNG().Bytes()
		if err != nil {
			return nil, fmt.Errorf("encode Windows icon (%dpx): %w", size, err)
		}
		entry := 6 + i*16
		if size < 256 {
			out[entry], out[entry+1] = byte(size), byte(size)
		}
		binary.LittleEndian.PutUint16(out[entry+4:entry+6], 1)  // planes
		binary.LittleEndian.PutUint16(out[entry+6:entry+8], 32) // bits per pixel
		binary.LittleEndian.PutUint32(out[entry+8:entry+12], uint32(len(payload)))
		binary.LittleEndian.PutUint32(out[entry+12:entry+16], offset)
		out = append(out, payload...)
		offset += uint32(len(payload))
	}
	return out, nil
}

func squareCropStd(img stdimage.Image) stdimage.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	side := cli.Min(w, h)
	x0 := b.Min.X + (w-side)/2
	y0 := b.Min.Y + (h-side)/2
	rect := stdimage.Rect(x0, y0, x0+side, y0+side)
	if sub, ok := img.(interface {
		SubImage(r stdimage.Rectangle) stdimage.Image
	}); ok {
		return sub.SubImage(rect)
	}
	dst := stdimage.NewNRGBA(stdimage.Rect(0, 0, side, side))
	draw.Draw(dst, dst.Bounds(), img, rect.Min, draw.Src)
	filled := stdimage.NewNRGBA(stdimage.Rect(0, 0, side, side))
	draw.Draw(filled, filled.Bounds(), dst, stdimage.Point{}, draw.Src)
	return filled
}
