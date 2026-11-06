package semcov

import (
	"fmt"
	"sync"
	"testing"
)

func TestResetDuringRecording(t *testing.T) {
	for name, record := range map[string]func(){
		"route":      func() { RecordRoute("GET", "/items") },
		"permission": func() { RecordPermission("items:read") },
		"entity":     func() { RecordEntityOp("items", "list") },
		"role":       func() { RecordRole("reader") },
		"event":      func() { RecordEvent("items.created") },
		"hook":       func() { RecordHook("items", "after_list") },
	} {
		t.Run(name, func(t *testing.T) {
			root := fresh(t)
			stop := make(chan struct{})
			failures := make(chan string, 4)
			var workers sync.WaitGroup
			for range 4 {
				workers.Go(func() {
					defer func() {
						if failure := recover(); failure != nil {
							failures <- fmt.Sprint(failure)
						}
					}()
					for {
						select {
						case <-stop:
							return
						default:
							record()
						}
					}
				})
			}
			for range 1000 {
				Reset()
				Enable(root)
			}
			close(stop)
			workers.Wait()
			close(failures)
			for failure := range failures {
				t.Errorf("record raced Reset: %s", failure)
			}
			record()
			if err := Flush(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
