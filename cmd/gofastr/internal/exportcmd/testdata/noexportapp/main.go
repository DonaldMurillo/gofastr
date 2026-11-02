// Command noexportapp is the fixture for a main that exits 0 without
// ever reaching App.Start, so nothing is exported: `gofastr export`
// must not report success for it.
package main

import "fmt"

func main() {
	fmt.Println("noexportapp: returning before Start")
}
