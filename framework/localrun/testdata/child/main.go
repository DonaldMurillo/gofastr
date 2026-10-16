// Command child prints whether localrun would register it. A test
// builds and starts it to check what a process started by a test
// binary inherits.
package main

import (
	"fmt"

	"github.com/DonaldMurillo/gofastr/framework/localrun"
)

func main() {
	fmt.Print(localrun.Disabled())
}
