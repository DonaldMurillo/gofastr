//go:build darwin && (arm64 || amd64)

package objc

import (
	"sync"
	"testing"
	"unsafe"
)

func TestBlocksRetainConcurrentLiterals(t *testing.T) {
	const workers, perWorker = 32, 128
	before := len(blocks)
	start := make(chan struct{})
	pointers := make(chan uintptr, workers*perWorker)
	var group sync.WaitGroup
	for range workers {
		group.Go(func() {
			<-start
			for range perWorker {
				pointers <- NewBlock(0)
			}
		})
	}
	close(start)
	group.Wait()
	close(pointers)
	if got := len(blocks) - before; got != workers*perWorker {
		t.Fatalf("retained %d blocks, want %d", got, workers*perWorker)
	}
	retained := make(map[uintptr]bool, workers*perWorker)
	for _, block := range blocks[before:] {
		retained[uintptr(unsafe.Pointer(block))] = true
	}
	for pointer := range pointers {
		if !retained[pointer] {
			t.Fatalf("block %x lost its Go liveness anchor", pointer)
		}
	}
}
