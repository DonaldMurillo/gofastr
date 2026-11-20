package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sync"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

var (
	stringsMu  sync.RWMutex
	sayPhrases = map[string]i18n.Phrase{}
)

// RegisterStrings declares phrases a runtime module says through __gofastr.say.
func RegisterStrings(phrases ...i18n.Phrase) {
	stringsMu.Lock()
	defer stringsMu.Unlock()
	for _, p := range phrases {
		if p.IsZero() {
			panic("runtime.RegisterStrings: a zero Phrase (define it with i18n.Define)")
		}
		if prev, ok := sayPhrases[p.Key()]; ok && prev != p {
			panic(fmt.Sprintf("runtime.RegisterStrings: %q registered twice with different English", p.Key()))
		}
		sayPhrases[p.Key()] = p
	}
}

// RegisteredStrings returns the registered phrases by key.
func RegisteredStrings() map[string]i18n.Phrase {
	stringsMu.RLock()
	defer stringsMu.RUnlock()
	return maps.Clone(sayPhrases)
}

// StringsJSON returns the #gofastr-strings block for ctx's locale, or nil.
func StringsJSON(ctx context.Context) []byte {
	if ctx == nil {
		return nil
	}
	stringsMu.RLock()
	keys := slices.Sorted(maps.Keys(sayPhrases))
	phrases := maps.Clone(sayPhrases)
	stringsMu.RUnlock()
	out := map[string]string{}
	for _, k := range keys {
		p := phrases[k]
		if s := p.Text(ctx); s != p.English() {
			out[k] = s
		}
	}
	if len(out) == 0 {
		return nil
	}
	buf, err := json.Marshal(out)
	if err != nil {
		return nil
	}
	return buf
}
