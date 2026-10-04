package registry

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
)

// A data block is an inert JSON document a package hands every page
// head: <script type="application/json" id="<id>">…</script>. It is
// how an optional package gives its browser module server-declared
// data without the host importing the package. core-ui/localdb's
// schema rides one: a host that never links localdb emits nothing, and
// its binary never carries localdb's code.
//
// The host (core-ui/widget's manifest scripts, framework/uihost's
// head) emits every registered block, sorted by id, on live pages and
// in export mode. A block whose function returns nil is skipped.

type dataBlock struct {
	id string
	fn func() []byte
}

var (
	dataBlocks  = map[string]dataBlock{}
	dataBlockID = regexp.MustCompile(`^gofastr-[a-z0-9-]{1,56}$`)
	// The ids the kernel and the host emit themselves; a registered
	// block under one would be a second element the runtime never
	// reads, or the one it reads in place of the real one.
	reservedDataBlockIDs = map[string]bool{
		"gofastr-signals": true, "gofastr-signals-partial": true,
		"gofastr-runtime-modules": true, "gofastr-behaviors": true,
		"gofastr-compute-assets": true, "gofastr-catalog": true,
	}
)

// RegisterDataBlock registers fn as the source of the data block id.
// id is "gofastr-" plus lowercase letters, digits and '-'. fn returns
// JSON (or nil for nothing to emit) and runs at render time, so it
// sees declarations made after registration. Registering an id twice
// panics.
func RegisterDataBlock(id string, fn func() []byte) {
	if !dataBlockID.MatchString(id) {
		panic(fmt.Sprintf("registry.RegisterDataBlock: id %q must match %s — it is an element id the runtime looks up", id, dataBlockID))
	}
	if fn == nil {
		panic("registry.RegisterDataBlock(" + id + "): fn must be non-nil")
	}
	mu.Lock()
	defer mu.Unlock()
	if reservedDataBlockIDs[id] {
		panic("registry.RegisterDataBlock(" + id + "): the id belongs to a block the kernel or the host emits itself")
	}
	if _, dup := dataBlocks[id]; dup {
		panic("registry.RegisterDataBlock(" + id + "): registered twice")
	}
	dataBlocks[id] = dataBlock{id: id, fn: fn}
}

// DataBlocksHTML renders every registered data block as an inert
// <script type="application/json"> element, sorted by id. The one
// sequence that could end an inline script, "</", is escaped as
// "<\/", which JSON reads back unchanged.
func DataBlocksHTML() string {
	mu.Lock()
	blocks := make([]dataBlock, 0, len(dataBlocks))
	for _, id := range slices.Sorted(maps.Keys(dataBlocks)) {
		blocks = append(blocks, dataBlocks[id])
	}
	mu.Unlock()
	var b strings.Builder
	for _, blk := range blocks {
		buf := blk.fn()
		if buf == nil {
			continue
		}
		b.WriteString(`<script type="application/json" id="` + blk.id + `">`)
		b.WriteString(strings.ReplaceAll(string(buf), `</`, `<\/`))
		b.WriteString(`</script>`)
	}
	return b.String()
}
