package update

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/filelock"
)

func TestFeedWaitsForOutputLock(t *testing.T) {
	if dir := os.Getenv("GOFASTR_TEST_FEED_LOCK"); dir != "" {
		fmt.Println("writer ready")
		if err := WriteFeed(dir, filepath.Join(dir, "update.key"), "1.2.0", "", "darwin-arm64", filepath.Join(dir, "Notes-1.2.0.zip"), "https://example.com/arm64.zip"); err != nil {
			t.Fatal(err)
		}
		return
	}
	dir := t.TempDir()
	_, _, pub, privateKey := writeKeyFile(t, dir)
	writeArchive(t, dir)
	lock, err := os.OpenFile(filepath.Join(dir, ".manifest.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := filelock.Lock(lock); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestFeedWaitsForOutputLock$")
	child.Env = append(os.Environ(), "GOFASTR_TEST_FEED_LOCK="+dir)
	child.Stderr = os.Stderr
	stdout, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	ready := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		reader := bufio.NewReader(stdout)
		line, _ := reader.ReadString('\n')
		ready <- line
		_, _ = io.Copy(io.Discard, reader)
		done <- child.Wait()
	}()
	select {
	case line := <-ready:
		if line != "writer ready\n" {
			t.Fatalf("child did not reach WriteFeed: %q", line)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case err := <-done:
		t.Fatalf("writer finished while another process held its output lock: %v", err)
	case <-time.After(250 * time.Millisecond):
	}
	if _, err := os.Stat(filepath.Join(dir, "manifest.json")); !os.IsNotExist(err) {
		t.Fatalf("waiting writer touched the manifest: %v", err)
	}
	entry := PlatformEntry{URL: "https://example.com/amd64.zip", SHA256: "retained", Size: 123}
	manifest, signature, err := SignFeed(Feed{Version: "1.2.0", Platforms: map[string]PlatformEntry{"darwin-amd64": entry}}, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"manifest.json": manifest, "manifest.json.sig": signature} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("waiting writer failed: %v", err)
	}
	manifest, err = os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	signature, err = os.ReadFile(filepath.Join(dir, "manifest.json.sig"))
	if err != nil {
		t.Fatal(err)
	}
	feed, err := ParseFeed(manifest, signature, pub)
	if err != nil {
		t.Fatal(err)
	}
	if len(feed.Platforms) != 2 || feed.Platforms["darwin-amd64"] != entry {
		t.Fatalf("writer did not merge the manifest written under the lock: %+v", feed.Platforms)
	}
}

func TestFeedConcurrentPlatformMerge(t *testing.T) {
	dir := t.TempDir()
	keyPath, _, pub, _ := writeKeyFile(t, dir)
	archive := writeArchive(t, dir)
	out := filepath.Join(dir, "feed")
	const writers = 16
	start := make(chan struct{})
	results := make(chan error, writers)
	var pending sync.WaitGroup
	for index := range writers {
		pending.Go(func() {
			<-start
			platform := fmt.Sprintf("darwin-arch%d", index)
			results <- WriteFeed(out, keyPath, "1.2.0", "", platform, archive, "https://example.com/"+platform+".zip")
		})
	}
	close(start)
	pending.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	manifest, err := os.ReadFile(filepath.Join(out, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	signature, err := os.ReadFile(filepath.Join(out, "manifest.json.sig"))
	if err != nil {
		t.Fatal(err)
	}
	feed, err := ParseFeed(manifest, signature, pub)
	if err != nil {
		t.Fatal(err)
	}
	if len(feed.Platforms) != writers {
		t.Fatalf("retained %d of %d platforms", len(feed.Platforms), writers)
	}
}
