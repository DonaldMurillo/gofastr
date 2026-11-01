package desktop

import (
	"context"
	"errors"
	"testing"
)

// A running bundle whose version is not semver ("1.0") reported "the
// update feed is invalid" on every check, blaming a good feed. It gets
// its own error, and the scheduled loop stops: no check can ever pass.
func TestCheckBadRunningVersionOwnError(t *testing.T) {
	feed := newUpdateFeedServer(t, "1.2.0")
	b, _ := updateTestBattery(t, feed.srv.URL+"/manifest.json", feed.pubHex, "1.0", "Notes.app", t.TempDir())
	_, err := b.CheckForUpdates(context.Background())
	if !errors.Is(err, errUpdateRunningVersion) {
		t.Fatalf("err = %v, want errUpdateRunningVersion", err)
	}
	if stop := b.updaterFor().checkAndNotify(context.Background()); !stop {
		t.Fatal("a running version no check can parse kept the scheduled loop alive")
	}
}
