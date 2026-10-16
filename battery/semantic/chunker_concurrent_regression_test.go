package semantic

import (
	"sync"
	"testing"
)

func TestLangAwareDefaultConfigurationConcurrent(t *testing.T) {
	l := &LangAware{}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() { defer wg.Done(); <-start; _, _ = l.Chunk(Document{ID: "doc", Text: "content"}) }()
	}
	close(start)
	wg.Wait()
}
