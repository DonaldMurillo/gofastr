package registry

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
)

type dataBlock struct {
	id string
	fn func() []byte
}

var (
	dataBlocks           = map[string]dataBlock{}
	dataBlockID          = regexp.MustCompile(`^gofastr-[a-z0-9-]{1,56}$`)
	reservedDataBlockIDs = map[string]bool{
		"gofastr-signals": true, "gofastr-signals-partial": true,
		"gofastr-signals-page":    true,
		"gofastr-runtime-modules": true, "gofastr-behaviors": true,
		"gofastr-compute-assets": true, "gofastr-catalog": true,
		"gofastr-routes": true, "gofastr-embed-root": true,
		"gofastr-embed-navigation-error": true, "gofastr-strings": true,
		"gofastr-locales": true,
	}
)

// RegisterDataBlock registers fn, run at render time, as the JSON source of block id.
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

// DataBlocksHTML renders every registered data block as inert JSON script elements.
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
		b.WriteString(strings.ReplaceAll(string(buf), `<`, `\u003c`))
		b.WriteString(`</script>`)
	}
	return b.String()
}
