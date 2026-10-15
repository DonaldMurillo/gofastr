package sitesearch

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// feed is a Source whose page says what word holds, and whose builds
// after the first can be held mid-render or made to fail.
type feed struct {
	word  atomic.Value
	calls atomic.Int32
	// hold, when set, blocks every build after the first until closed;
	// entered closes when such a build reaches the source.
	hold, entered chan struct{}
	enterOnce     sync.Once
	releaseOnce   sync.Once
	fail          atomic.Bool
}

// release lets held builds through.
func (f *feed) release() {
	if f.hold != nil {
		f.releaseOnce.Do(func() { close(f.hold) })
	}
}

func newFeed(word string) *feed {
	f := &feed{}
	f.word.Store(word)
	return f
}

func (f *feed) source(ctx context.Context) ([]Page, error) {
	if f.calls.Add(1) > 1 {
		if f.entered != nil {
			f.enterOnce.Do(func() { close(f.entered) })
		}
		if f.hold != nil {
			select {
			case <-f.hold:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		if f.fail.Load() {
			return nil, errors.New("feed down")
		}
	}
	return []Page{{URL: "/feed", Title: "Feed", Sections: []Section{{Text: f.word.Load().(string)}}}}, nil
}

func staleSite(t *testing.T, f *feed, maxAge time.Duration) *Battery {
	t.Helper()
	// A test that fails partway must not leave a build held.
	t.Cleanup(f.release)
	bat := New(Config{MaxAge: maxAge, Sources: []Source{f.source}})
	app := newSite(t, bat)
	if err := app.InitPlugins(); err != nil {
		t.Fatal(err)
	}
	if _, err := bat.Index(context.Background()); err != nil {
		t.Fatal(err)
	}
	return bat
}

// age makes the live index older than any MaxAge.
func age(b *Battery) {
	b.mu.Lock()
	b.builtAt = b.builtAt.Add(-24 * time.Hour)
	b.mu.Unlock()
}

// settle waits for the background rebuild in flight, if any.
func settle(t *testing.T, b *Battery) {
	t.Helper()
	b.life.Lock()
	done := b.rebuilt
	b.life.Unlock()
	if done == nil {
		return
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("background rebuild never finished")
	}
}

func finds(t *testing.T, b *Battery, word string) bool {
	t.Helper()
	idx, err := b.Index(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return len(idx.Search(word, 5)) > 0
}

// A stale index answers at once and rebuilds behind the query: the
// query never waits for the site to render again.
func TestStaleQueryServesOldIndex(t *testing.T) {
	f := newFeed("walrus")
	f.hold, f.entered = make(chan struct{}), make(chan struct{})
	bat := staleSite(t, f, time.Minute)
	f.word.Store("narwhal")
	age(bat)

	answered := make(chan bool, 1)
	go func() { answered <- finds(t, bat, "walrus") }()
	select {
	case old := <-answered:
		if !old {
			t.Error("the stale query lost the old index")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a stale query waited for the rebuild")
	}
	select {
	case <-f.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("a stale query started no rebuild")
	}
	f.release()
	settle(t, bat)
	if !finds(t, bat, "narwhal") {
		t.Error("the rebuilt index lacks the new content")
	}
}

// An index younger than MaxAge, or a zero MaxAge, never rebuilds.
func TestFreshIndexDoesNotRebuild(t *testing.T) {
	for _, maxAge := range []time.Duration{0, time.Hour} {
		f := newFeed("walrus")
		bat := staleSite(t, f, maxAge)
		if maxAge == 0 {
			age(bat)
		}
		finds(t, bat, "walrus")
		settle(t, bat)
		if n := f.calls.Load(); n != 1 {
			t.Errorf("MaxAge %v: %d builds, want 1", maxAge, n)
		}
	}
}

// Many stale queries start one rebuild between them.
func TestStaleQueriesStartOneBuild(t *testing.T) {
	f := newFeed("walrus")
	f.hold, f.entered = make(chan struct{}), make(chan struct{})
	bat := staleSite(t, f, time.Minute)
	age(bat)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() { finds(t, bat, "walrus") })
	}
	wg.Wait()
	<-f.entered
	f.release()
	settle(t, bat)
	if n := f.calls.Load(); n != 2 {
		t.Errorf("eight stale queries ran %d builds in all, want 2", n)
	}
}

// A rebuild that fails keeps the old index serving, and waits
// buildRetry before another query tries again.
func TestFailedRebuildKeepsOldIndex(t *testing.T) {
	f := newFeed("walrus")
	bat := staleSite(t, f, time.Minute)
	f.fail.Store(true)
	age(bat)
	finds(t, bat, "walrus")
	settle(t, bat)
	if !finds(t, bat, "walrus") {
		t.Error("a failed rebuild dropped the old index")
	}
	settle(t, bat)
	if n := f.calls.Load(); n != 2 {
		t.Errorf("%d builds, want 2: a failed rebuild must wait buildRetry", n)
	}
}

// Refresh builds beside the live index and swaps it in: a query during
// the refresh answers from the old index.
func TestRefreshDoesNotBlockQueries(t *testing.T) {
	f := newFeed("walrus")
	f.hold, f.entered = make(chan struct{}), make(chan struct{})
	bat := staleSite(t, f, 0)
	f.word.Store("narwhal")
	refreshed := make(chan error, 1)
	go func() { refreshed <- bat.Refresh(context.Background()) }()
	<-f.entered
	answered := make(chan bool, 1)
	go func() { answered <- finds(t, bat, "walrus") }()
	select {
	case old := <-answered:
		if !old {
			t.Error("a query during Refresh lost the old index")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a query waited for Refresh")
	}
	f.release()
	if err := <-refreshed; err != nil {
		t.Fatal(err)
	}
	if !finds(t, bat, "narwhal") {
		t.Error("Refresh did not swap in the new index")
	}
}

// OnStop ends a rebuild in flight and waits for it, and a stale query
// after OnStop starts none.
func TestStopEndsRebuild(t *testing.T) {
	f := newFeed("walrus")
	f.hold, f.entered = make(chan struct{}), make(chan struct{})
	bat := staleSite(t, f, time.Minute)
	age(bat)
	finds(t, bat, "walrus")
	<-f.entered
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := bat.OnStop(ctx); err != nil {
		t.Fatalf("OnStop with a rebuild in flight: %v", err)
	}
	// A rebuild starts inside the query, so the handle tells at once.
	bat.life.Lock()
	before := bat.rebuilt
	bat.life.Unlock()
	finds(t, bat, "walrus")
	bat.life.Lock()
	after := bat.rebuilt
	bat.life.Unlock()
	if after != before {
		t.Error("a stale query after OnStop started a rebuild")
	}
}

func TestNegativeMaxAgePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("New accepted a negative MaxAge")
		}
	}()
	New(Config{MaxAge: -time.Second})
}
