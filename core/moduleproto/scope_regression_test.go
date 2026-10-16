package moduleproto

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestResponseRejectsBothOutcomes(t *testing.T) {
	var f Frame
	if json.Unmarshal([]byte(`{"jsonrpc":"2.0","id":1,"result":null,"error":{"code":1,"message":"bad"}}`), &f) == nil {
		t.Fatal("accepted result and error")
	}
}
func TestNotificationRejectsNullID(t *testing.T) {
	var f Frame
	if json.Unmarshal([]byte(`{"jsonrpc":"2.0","id":null,"method":"ping"}`), &f) == nil {
		t.Fatal("null id read as absent")
	}
}
func TestNilSuccessResultEncodesNull(t *testing.T) {
	b, e := json.Marshal(NewSuccessResponse(1, nil))
	if e != nil {
		t.Fatal(e)
	}
	var f Frame
	if e = json.Unmarshal(b, &f); e != nil {
		t.Fatalf("constructor produced invalid response: %s %v", b, e)
	}
}

type scopeShortWriter struct{}

func (scopeShortWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }
func TestCodecDetectsShortWrite(t *testing.T) {
	c, _ := NewCodec(strings.NewReader(""), scopeShortWriter{}, 0)
	if e := c.WriteFrame(NewNotification("ping", nil)); !errors.Is(e, io.ErrShortWrite) {
		t.Fatalf("error=%v", e)
	}
}
func TestCodecReadsExactStructuralLimit(t *testing.T) {
	prefix := `{"jsonrpc":"2.0","method":"ping","params":"`
	suffix := `"}`
	raw := prefix + strings.Repeat("x", scannerMaxCap-len(prefix)-len(suffix)) + suffix
	var out bytes.Buffer
	c, e := NewCodec(strings.NewReader(raw+"\n"), &out, scannerMaxCap)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = c.ReadFrame(); e != nil {
		t.Fatal(e)
	}
}
