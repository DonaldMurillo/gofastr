// Command otherrunapp writes an export receipt naming a run that is not
// the one gofastr export started.
package main

import (
	"os"
	"path/filepath"
)

func main() {
	receipt := filepath.Join(os.Getenv("GOFASTR_EXPORT"), ".gofastr-export.json")
	if err := os.WriteFile(receipt, []byte(`{"run":"another-run","pages":3}`), 0o600); err != nil {
		panic(err)
	}
}
