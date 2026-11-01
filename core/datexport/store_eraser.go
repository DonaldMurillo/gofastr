package datexport

import (
	"context"
	"fmt"
	"slices"
	"sort"
)

// StoreEraser removes a user's data from a store outside the SQL database:
// a file the process writes (battery/semantic's snapshot and WAL), or a
// Redis key space (battery/queue's RedisQueue). A DataEraser cannot reach
// such a store, since the framework only builds SQL. App.EraseUserData runs
// every registered StoreEraser after the SQL erasure commits.
//
//   - Name is the report label. Every registration stays until its
//     unregister func runs: one registered while another of the same
//     Name is live is listed as Name#2 (the first free #n), so two
//     stores of one kind, such as two RedisQueues with one queue name on
//     different servers, are both erased.
//   - Source is the owning module ("semantic", "queue"), recorded in the
//     report.
//   - Erase removes everything in the store that names userID and returns
//     how many items it removed. With dryRun it removes nothing and returns
//     how many it would. A re-run finds nothing and returns 0.
//
// Erase runs after the commit, so its failure cannot roll the SQL erasure
// back: EraseUserData returns an error naming the store, and the caller
// erases again once the store is reachable.
type StoreEraser struct {
	Name   string
	Source string
	Erase  func(ctx context.Context, userID string, dryRun bool) (int, error)
}

// storeErasers holds the registered entries. Each is a distinct pointer,
// so an unregister func removes its own registration and no other.
var storeErasers []*StoreEraser

// RegisterStoreEraser adds e and returns the name it is listed under and
// the func that removes it. A store calls the func when it closes, so an
// erasure never reaches a closed store; until then the registration
// stays, and it keeps the store reachable. A Name already live is
// suffixed "#2", "#3", ... (the first free one). An empty Name or a nil
// Erase panics: the registration is developer code, and a silent skip
// would leave the store out of every erasure. Calling the returned func
// again does nothing.
func RegisterStoreEraser(e StoreEraser) (name string, unregister func()) {
	if e.Name == "" || e.Erase == nil {
		panic(fmt.Sprintf("datexport: RegisterStoreEraser: %q needs a Name and an Erase func", e.Name))
	}
	entry := &e
	mu.Lock()
	defer mu.Unlock()
	taken := func(name string) bool {
		return slices.ContainsFunc(storeErasers, func(x *StoreEraser) bool { return x.Name == name })
	}
	base := e.Name
	for n := 2; taken(entry.Name); n++ {
		entry.Name = fmt.Sprintf("%s#%d", base, n)
	}
	storeErasers = append(storeErasers, entry)
	return entry.Name, func() {
		mu.Lock()
		defer mu.Unlock()
		storeErasers = slices.DeleteFunc(storeErasers, func(x *StoreEraser) bool { return x == entry })
	}
}

// AllStoreErasers returns a copy of the registered store erasers sorted by
// Name.
func AllStoreErasers() []StoreEraser {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]StoreEraser, 0, len(storeErasers))
	for _, e := range storeErasers {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
