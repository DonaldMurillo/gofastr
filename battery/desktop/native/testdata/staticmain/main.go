// Command staticmain links battery/desktop/native, for the static-build
// test: it never opens a window.
package main

import (
	"fmt"

	"github.com/DonaldMurillo/gofastr/battery/desktop/native"
)

func main() { fmt.Println(native.Shell() != nil) }
