package framework

import (
	"context"
	"testing"
	"time"
)

type reentrantModuleCoordinator struct {
	desired func() bool
	info    func() (ProcessModuleInfo, bool)
}

func (reentrantModuleCoordinator) Reconcile(string) {}
func (c reentrantModuleCoordinator) DesiredEnabled(string) bool {
	return c.desired != nil && c.desired()
}
func (reentrantModuleCoordinator) SetDesiredEnabled(context.Context, string, bool) error {
	return nil
}
func (c reentrantModuleCoordinator) LocalInfo(string) (ProcessModuleInfo, bool) {
	if c.info == nil {
		return ProcessModuleInfo{}, false
	}
	return c.info()
}

func TestModuleCoordinatorReadsReenterManager(t *testing.T) {
	const name = "example"
	for _, read := range []string{"Enabled", "ListDesired", "ListInfo"} {
		t.Run(read, func(t *testing.T) {
			mm := NewModuleManager(nil, nil)
			mm.register(name, ModuleManifest{})
			pc := reentrantModuleCoordinator{}
			if read == "ListInfo" {
				pc.info = func() (ProcessModuleInfo, bool) {
					mm.SetProcessCoordinator(nil)
					return ProcessModuleInfo{ObservedGeneration: 7}, true
				}
			} else {
				mm.markProcess(name)
				pc.desired = func() bool {
					mm.SetProcessCoordinator(nil)
					return true
				}
			}
			mm.SetProcessCoordinator(pc)

			result := make(chan bool, 1)
			go func() {
				if read == "Enabled" {
					result <- mm.Enabled(name)
					return
				}
				list := mm.List()
				result <- len(list) == 1 && list[0].Name == name && list[0].Enabled &&
					(read != "ListInfo" || list[0].ObservedGeneration == 7)
			}()
			select {
			case ok := <-result:
				if !ok {
					t.Fatal("module read did not report the coordinator result")
				}
			case <-time.After(time.Second):
				t.Fatal("module read did not return after the coordinator re-entered the manager")
			}
		})
	}
}
