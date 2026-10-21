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

// Runtime strings: the words the browser runtime says on its own, a
// toast after a failed request or the fallback notice's dismiss button.
// The JS writes each one as say('key', 'English', params): the English
// is the fallback when the page carries no translation, and the host
// renders a #gofastr-strings block per request holding the translations
// for the reader's locale (StringsJSON). TestRuntimeSayCallsMatchPhrases
// holds every say() call in the repo's JS to a registered phrase with
// the same English, so the two cannot drift.

var (
	stringsMu  sync.RWMutex
	sayPhrases = map[string]i18n.Phrase{}
)

// RegisterStrings declares phrases a runtime module says through
// __gofastr.say, so the host's #gofastr-strings block carries their
// translations. A package that ships JS with say() calls registers its
// phrases at package level, beside the i18n.Define calls. Registering
// the same phrase twice is a no-op; a different phrase under a key
// already registered panics, since the block could carry only one.
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

// StringsJSON is the #gofastr-strings block's content for ctx: each
// registered phrase whose text in ctx's locale differs from its English,
// keyed by phrase key. nil when nothing differs, which is every page
// with no translator or an English reader, so those pages carry no
// block and say() answers the English written beside it.
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
		// Translated with no params: the placeholders travel to the
		// browser, which fills them where the value is known.
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
