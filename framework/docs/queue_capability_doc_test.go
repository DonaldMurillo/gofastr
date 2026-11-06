package docs

import (
	"strings"
	"testing"
)

func TestQueueDocumentsMemoryDeadLetterBrowsing(t *testing.T) {
	document, err := Get("queue")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(document), "| `Browsable` | Yes (dead-letter only) | Yes | Yes (dead-letter only) |") {
		t.Fatal("queue capability table must mark both MemoryQueue and RedisQueue as dead-letter-only")
	}
}
