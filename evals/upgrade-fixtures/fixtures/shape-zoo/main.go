// Command shape-zoo holds one instance of each code shape a reviewer
// found the upgrade scanner reading wrong. Lines that must be reported
// carry a marker comment naming the note; every other line must stay
// silent. TestShapeZoo in evals/upgrade-fixtures compares the two.
package main

import (
	"fmt"

	"example.com/shape-zoo/pages"
	"example.com/shape-zoo/store"
	"github.com/DonaldMurillo/gofastr/core/middleware"
)

// The assertion names neither Finish nor the response type, so only the
// compile error reaches it.
var _ middleware.IdempotencyStore = (*store.Store)(nil) // zoo:err finish

func main() {
	fmt.Println(len(pages.All()))
}
