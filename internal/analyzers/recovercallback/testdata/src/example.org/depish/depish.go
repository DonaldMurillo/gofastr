// Package depish stands in for a third-party dependency's interface
// (a golang.org/x/... shape): a dotted module path OUTSIDE this
// module, so its methods are not this repo's extension points however
// they are held.
package depish

import "context"

type Dep interface {
	Do(ctx context.Context) error
}
