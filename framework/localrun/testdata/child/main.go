// Command child prints whether localrun would register it.
package main

import (
	"fmt"

	"github.com/DonaldMurillo/gofastr/framework/localrun"
)

func main() {
	fmt.Print(localrun.Disabled())
}
