package file_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/file"
)

func TestWebPRefusesShortRIFF(t *testing.T) {
	valid := buildWebP(t, webpChunk{typ: "VP8L", payload: []byte("fake-vp8l-data")})
	for size := uint32(0); size < 12; size++ {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			body := bytes.Clone(valid)
			binary.LittleEndian.PutUint32(body[4:8], size)
			store := &captureStorage{}
			_, err := file.ProcessFileField(context.Background(), store,
				bytes.NewReader(body), "photo.webp", "posts", "photo", file.StripMetadata())
			if err == nil || len(store.data) != 0 {
				t.Fatalf("RIFF size %d: error=%v, stored %d bytes; want refusal before storage", size, err, len(store.data))
			}
		})
	}
	store := &captureStorage{}
	if _, err := file.ProcessFileField(context.Background(), store,
		bytes.NewReader(valid), "photo.webp", "posts", "photo", file.StripMetadata()); err != nil {
		t.Fatalf("valid container refused: %v", err)
	}
	if !bytes.Equal(store.data, valid) {
		t.Fatal("valid container changed")
	}
}
