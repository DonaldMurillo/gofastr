package chromedptest

import (
	"context"
	"fmt"
	"time"

	"github.com/chromedp/chromedp"
)

// Poll evaluates expr in the page until it resolves to true or the
// timeout passes, sleeping 200ms between attempts. It returns the
// chromedp error from the failing evaluation, or a timeout error naming
// expr; the caller supplies the failure label.
func Poll(ctx context.Context, expr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		var ok bool
		if err := chromedp.Run(ctx, chromedp.Evaluate(expr, &ok)); err != nil {
			return err
		}
		if ok {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for %s", expr)
		}
		time.Sleep(200 * time.Millisecond)
	}
}
