package runtime

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

func loadingProbe(ctx context.Context, delay time.Duration, id string) (string, error) {
	var skeleton string
	err := chromedp.Run(ctx,
		chromedp.Sleep(delay),
		chromedp.Evaluate(fmt.Sprintf(`String(document.getElementById(%q) !== null)`, id), &skeleton),
	)
	return skeleton, err
}

func TestLoadingProbeRejectsReadFailure(t *testing.T) {
	ctx, cancel := chromedp.NewContext(context.Background())
	cancel()
	if value, err := loadingProbe(ctx, time.Millisecond, "missing"); err == nil {
		t.Fatalf("failed browser read returned %q without an error", value)
	}
}
