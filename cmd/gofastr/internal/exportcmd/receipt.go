package exportcmd

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/mod/semver"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/project"
	"github.com/DonaldMurillo/gofastr/framework"
)

const firstReceiptVersion = "v0.96.0"

const maxReceiptBytes = 4 << 10

type receiptPolicy int

const (
	receiptUnknown receiptPolicy = iota
	receiptRequired
)

func (p receiptPolicy) String() string {
	if p == receiptRequired {
		return "required"
	}
	return "unknown"
}

func exportReceiptPolicy(pkg string) receiptPolicy {
	dir := "."
	if info, err := os.Stat(pkg); err == nil && info.IsDir() {
		dir = pkg
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return receiptUnknown
	}
	mod, root := project.FindEnclosingGoMod(abs)
	if root == "" {
		return receiptUnknown
	}
	if mod == project.GofastrModule || strings.HasPrefix(mod, project.GofastrModule+"/") {
		return receiptRequired
	}
	v, replaced, err := project.GoModGofastrVersion(root)
	if err != nil || replaced || v == "" || !semver.IsValid(v) {
		return receiptUnknown
	}
	if semver.Compare(v, firstReceiptVersion) >= 0 {
		return receiptRequired
	}
	return receiptUnknown
}

func newExportRun() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("export: mint run id: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func clearReceipt(dir string) error {
	err := os.Remove(filepath.Join(dir, framework.ExportReceiptFile))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("export: remove the earlier receipt: %w", err)
	}
	return nil
}

func readReceipt(dir string) (run string, pages int, ok bool, err error) {
	path := filepath.Join(dir, framework.ExportReceiptFile)
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", 0, false, nil
	}
	if err != nil {
		return "", 0, false, err
	}
	var rec struct {
		Run   string `json:"run"`
		Pages int    `json:"pages"`
	}
	decodeErr := json.NewDecoder(io.LimitReader(f, maxReceiptBytes)).Decode(&rec)
	closeErr := f.Close()
	if decodeErr != nil {
		return "", 0, false, fmt.Errorf("read %s: %w", framework.ExportReceiptFile, decodeErr)
	}
	if closeErr != nil {
		return "", 0, false, closeErr
	}
	if err := os.Remove(path); err != nil {
		return "", 0, false, err
	}
	return rec.Run, rec.Pages, true, nil
}
