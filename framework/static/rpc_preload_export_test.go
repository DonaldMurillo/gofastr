package static

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

type rpcButtonScreen struct{}

func (rpcButtonScreen) Render() render.HTML {
	return render.HTML(`<button data-cui-rpc="/api/save">Save</button><div data-cui-poll="10s" data-cui-poll-src="/p">x</div>`)
}

// An exported page never loads rpc.js: the static runtime's rpc stub owns
// every data-cui-rpc control and boot skips the rpc marker. Preloading it
// was a wasted fetch on every such page and a "preloaded but not used"
// console warning. Other demand modules the page uses keep their preload.
func TestExportDoesNotPreloadRPC(t *testing.T) {
	a := coreapp.NewApp("SSGRPC")
	a.Register("/", rpcButtonScreen{}, nil)
	out := t.TempDir()
	if _, err := (&Builder{Host: uihost.New(a), OutDir: out}).Build(context.Background()); err != nil {
		t.Fatalf("Build: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(out, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	page := string(b)
	if !strings.Contains(page, `data-cui-rpc="/api/save"`) {
		t.Fatalf("fixture lost its rpc control: %s", page)
	}
	if strings.Contains(page, `/__gofastr/runtime/rpc.js`) {
		t.Errorf("exported page preloads rpc.js, which the static runtime never loads")
	}
	if !strings.Contains(page, `/__gofastr/runtime/poll.js`) {
		t.Errorf("exported page lost the poll preload it does use")
	}
}
