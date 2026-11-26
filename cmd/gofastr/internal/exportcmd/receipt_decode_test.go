package exportcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

func TestReceiptRejectsTrailingOrOversizedData(t *testing.T) {
	for name, suffix := range map[string]string{
		"second JSON value": "{}",
		"oversized":         strings.Repeat(" ", maxReceiptBytes),
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			body := `{"run":"test","pages":1}` + suffix
			if err := os.WriteFile(filepath.Join(dir, framework.ExportReceiptFile), []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, _, _, err := readReceipt(dir); err == nil {
				t.Fatal("accepted invalid receipt")
			}
		})
	}
}
