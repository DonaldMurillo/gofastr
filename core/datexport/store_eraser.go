package datexport

import (
	"context"
	"fmt"
	"slices"
	"sort"
)

// StoreEraser removes a user's data from a store outside SQL; set Erase or EraseResolved.
type StoreEraser struct {
	Name          string
	Source        string
	Erase         func(ctx context.Context, userID string, dryRun bool) (int, error)
	EraseResolved func(ctx context.Context, user ErasedUser, dryRun bool) (int, error)
	Producer      bool
}

// ErasedUser is the erased user's id and each identity that resolved.
type ErasedUser struct {
	ID         string
	Identities map[IdentityKind]string
}

// Identity returns the resolved value of kind and whether it resolved.
func (u ErasedUser) Identity(kind IdentityKind) (string, bool) {
	v, ok := u.Identities[kind]
	return v, ok && v != ""
}

var storeErasers []*StoreEraser

// RegisterStoreEraser adds e under a free name and returns it with e's own unregister func.
func RegisterStoreEraser(e StoreEraser) (name string, unregister func()) {
	if e.Name == "" || (e.Erase == nil) == (e.EraseResolved == nil) {
		panic(fmt.Sprintf("datexport: RegisterStoreEraser: %q needs a Name and exactly one of Erase and EraseResolved", e.Name))
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

// AllStoreErasers returns the store erasers in run order, producers first.
func AllStoreErasers() []StoreEraser {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]StoreEraser, 0, len(storeErasers))
	for _, e := range storeErasers {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Producer != out[j].Producer {
			return out[i].Producer
		}
		return out[i].Name < out[j].Name
	})
	return out
}
